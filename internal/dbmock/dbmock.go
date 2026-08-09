// Package dbmock provides a scriptable database/sql driver for tests. database/sql talks to a
// database through the driver interfaces, so a driver written in plain Go yields a real *sql.DB
// backed entirely by memory: no server, no file, and no third-party package.
//
// It exists to reach the failure branches a working database will not produce on demand: a
// query that fails, a row that will not scan, an iteration that breaks partway (which is the
// only way to reach a rows.Err() check), and a transaction that fails to begin or commit.
package dbmock

import (
	"database/sql"
	"database/sql/driver"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

// Result is what a statement reports after an Exec.
type Result struct {
	LastInsertID    int64
	Rows            int64
	LastInsertIDErr error
	RowsErr         error
}

// Response is what the driver returns for one statement. Err fails the statement outright;
// otherwise Query serves Columns and Values, and IterErr breaks the iteration once they run
// out, which surfaces to the caller as rows.Err().
type Response struct {
	Err     error
	Columns []string
	Values  [][]driver.Value
	IterErr error
	Result  Result
}

// Config scripts a connection. Responses are consumed in order by each Query or Exec, and
// Fallback serves every statement after they run out. The remaining fields fail the connection
// level operations.
type Config struct {
	Responses  []Response
	Fallback   Response
	PrepareErr error
	// PrepareErrs fails individual prepares in order, a nil entry succeeding, which is how a
	// test reaches the second or third prepare of a transaction. PrepareErr serves the rest.
	PrepareErrs []error
	BeginErr    error
	CommitErr   error
	RollbackErr error
	CloseErr    error
}

var registrations atomic.Int64

// Open registers a driver for this config and returns a database handle bound to it. Each call
// registers under its own name, so tests never collide. The pool is held at one connection so
// responses are consumed in the order the test wrote them.
func Open(config Config) *sql.DB {
	name := "dbmock-" + strconv.FormatInt(registrations.Add(1), 10)
	sql.Register(name, mockDriver{state: &state{config: config}})

	db, _ := sql.Open(name, "")
	db.SetMaxOpenConns(1)

	return db
}

// state is shared by every connection the pool opens, so the response queue is one sequence.
type state struct {
	mu          sync.Mutex
	config      Config
	next        int
	nextPrepare int
}

func (s *state) prepareErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.nextPrepare >= len(s.config.PrepareErrs) {
		return s.config.PrepareErr
	}

	err := s.config.PrepareErrs[s.nextPrepare]
	s.nextPrepare++

	return err
}

func (s *state) response() Response {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.next >= len(s.config.Responses) {
		return s.config.Fallback
	}

	response := s.config.Responses[s.next]
	s.next++

	return response
}

type mockDriver struct{ state *state }

func (d mockDriver) Open(string) (driver.Conn, error) {
	return mockConn(d), nil
}

type mockConn struct{ state *state }

func (c mockConn) Prepare(string) (driver.Stmt, error) {
	if err := c.state.prepareErr(); err != nil {
		return nil, err
	}

	return mockStmt(c), nil
}

// Exec and Query make the connection a driver.Execer and driver.Queryer, as the real drivers
// are. Without them database/sql prepares a statement for every call, which would make a
// scripted Prepare failure fire on statements the caller never prepared itself.
func (c mockConn) Exec(string, []driver.Value) (driver.Result, error) {
	return mockStmt(c).Exec(nil)
}

func (c mockConn) Query(string, []driver.Value) (driver.Rows, error) {
	return mockStmt(c).Query(nil)
}

func (c mockConn) Close() error { return c.state.config.CloseErr }

func (c mockConn) Begin() (driver.Tx, error) {
	if c.state.config.BeginErr != nil {
		return nil, c.state.config.BeginErr
	}

	return mockTx(c), nil
}

type mockStmt struct{ state *state }

func (mockStmt) Close() error  { return nil }
func (mockStmt) NumInput() int { return -1 }

func (s mockStmt) Exec([]driver.Value) (driver.Result, error) {
	response := s.state.response()
	if response.Err != nil {
		return nil, response.Err
	}

	return mockResult{result: response.Result}, nil
}

func (s mockStmt) Query([]driver.Value) (driver.Rows, error) {
	response := s.state.response()
	if response.Err != nil {
		return nil, response.Err
	}

	return &mockRows{columns: response.Columns, values: response.Values, iterErr: response.IterErr}, nil
}

type mockRows struct {
	columns []string
	values  [][]driver.Value
	iterErr error
	pos     int
}

func (r *mockRows) Columns() []string { return r.columns }
func (*mockRows) Close() error        { return nil }

func (r *mockRows) Next(dest []driver.Value) error {
	if r.pos < len(r.values) {
		copy(dest, r.values[r.pos])
		r.pos++

		return nil
	}

	if r.iterErr != nil {
		return r.iterErr
	}

	return io.EOF
}

type mockTx struct{ state *state }

func (t mockTx) Commit() error   { return t.state.config.CommitErr }
func (t mockTx) Rollback() error { return t.state.config.RollbackErr }

type mockResult struct{ result Result }

func (r mockResult) LastInsertId() (int64, error) {
	return r.result.LastInsertID, r.result.LastInsertIDErr
}

func (r mockResult) RowsAffected() (int64, error) {
	return r.result.Rows, r.result.RowsErr
}
