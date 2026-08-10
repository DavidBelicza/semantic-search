package dbmock

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

var errScripted = errors.New("scripted failure")

func TestQueryServesScriptedRows(t *testing.T) {
	db := Open(Config{Responses: []Response{{
		Columns: []string{"id", "name"},
		Values:  [][]driver.Value{{int64(1), "ada"}, {int64(2), "alan"}},
	}}})
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT id, name FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	if len(names) != 2 || names[0] != "ada" || names[1] != "alan" {
		t.Fatalf("got %v", names)
	}
}

func TestQueryReportsAScriptedFailure(t *testing.T) {
	db := Open(Config{Fallback: Response{Err: errScripted}})
	defer db.Close()

	_, err := db.QueryContext(context.Background(), "SELECT 1")
	if !errors.Is(err, errScripted) {
		t.Fatalf("got %v", err)
	}
}

func TestIterationCanBreakPartwayToReachRowsErr(t *testing.T) {
	db := Open(Config{Responses: []Response{{
		Columns: []string{"id"},
		Values:  [][]driver.Value{{int64(1)}},
		IterErr: errScripted,
	}}})
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT id FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
	}

	if !errors.Is(rows.Err(), errScripted) {
		t.Fatalf("want the scripted iteration failure, got %v", rows.Err())
	}
}

func TestScanFailsOnAMistypedValue(t *testing.T) {
	db := Open(Config{Responses: []Response{{
		Columns: []string{"id"},
		Values:  [][]driver.Value{{"not-a-number"}},
	}}})
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT id FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("want a row")
	}

	var id int64
	if err := rows.Scan(&id); err == nil {
		t.Fatal("want a scan failure")
	}
}

func TestResponsesAreConsumedInOrderThenFallback(t *testing.T) {
	db := Open(Config{
		Responses: []Response{{Result: Result{Rows: 1}}, {Err: errScripted}},
		Fallback:  Response{Result: Result{Rows: 7}},
	})
	defer db.Close()

	ctx := context.Background()

	first, err := db.ExecContext(ctx, "UPDATE t SET a = 1")
	if err != nil {
		t.Fatalf("first exec: %v", err)
	}
	if affected, _ := first.RowsAffected(); affected != 1 {
		t.Fatalf("first affected: %d", affected)
	}

	if _, err := db.ExecContext(ctx, "UPDATE t SET a = 2"); !errors.Is(err, errScripted) {
		t.Fatalf("second exec: %v", err)
	}

	third, err := db.ExecContext(ctx, "UPDATE t SET a = 3")
	if err != nil {
		t.Fatalf("third exec: %v", err)
	}
	if affected, _ := third.RowsAffected(); affected != 7 {
		t.Fatalf("want the fallback, got %d", affected)
	}
}

func TestResultReportsScriptedValuesAndFailures(t *testing.T) {
	db := Open(Config{Fallback: Response{Result: Result{
		LastInsertID: 42, Rows: 3,
	}}})
	defer db.Close()

	result, err := db.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil || id != 42 {
		t.Fatalf("last insert id: %d %v", id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 3 {
		t.Fatalf("rows affected: %d %v", rows, err)
	}
}

func TestResultCanFailBothReports(t *testing.T) {
	db := Open(Config{Fallback: Response{Result: Result{
		LastInsertIDErr: errScripted, RowsErr: errScripted,
	}}})
	defer db.Close()

	result, err := db.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	if _, err := result.LastInsertId(); !errors.Is(err, errScripted) {
		t.Fatalf("last insert id: %v", err)
	}
	if _, err := result.RowsAffected(); !errors.Is(err, errScripted) {
		t.Fatalf("rows affected: %v", err)
	}
}

func TestTransactionsCanFailToBeginCommitAndRollback(t *testing.T) {
	ctx := context.Background()

	failBegin := Open(Config{BeginErr: errScripted})
	defer failBegin.Close()
	if _, err := failBegin.BeginTx(ctx, nil); !errors.Is(err, errScripted) {
		t.Fatalf("begin: %v", err)
	}

	failCommit := Open(Config{CommitErr: errScripted})
	defer failCommit.Close()
	tx, err := failCommit.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tx.Commit(); !errors.Is(err, errScripted) {
		t.Fatalf("commit: %v", err)
	}

	failRollback := Open(Config{RollbackErr: errScripted})
	defer failRollback.Close()
	rollbackTx, err := failRollback.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := rollbackTx.Rollback(); !errors.Is(err, errScripted) {
		t.Fatalf("rollback: %v", err)
	}
}

func TestPrepareCanFail(t *testing.T) {
	db := Open(Config{PrepareErr: errScripted})
	defer db.Close()

	if _, err := db.PrepareContext(context.Background(), "SELECT 1"); !errors.Is(err, errScripted) {
		t.Fatalf("prepare: %v", err)
	}
}

func TestPrepareSucceedsAndServesAResponse(t *testing.T) {
	db := Open(Config{Fallback: Response{Result: Result{Rows: 2}}})
	defer db.Close()

	stmt, err := db.PrepareContext(context.Background(), "UPDATE t SET a = ?")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	result, err := stmt.ExecContext(context.Background(), 1)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if affected, _ := result.RowsAffected(); affected != 2 {
		t.Fatalf("got %d", affected)
	}
}

func TestPreparedStatementServesRows(t *testing.T) {
	db := Open(Config{Fallback: Response{
		Columns: []string{"id"},
		Values:  [][]driver.Value{{int64(5)}},
	}})
	defer db.Close()

	stmt, err := db.PrepareContext(context.Background(), "SELECT id FROM t WHERE id = ?")
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	defer stmt.Close()

	rows, err := stmt.QueryContext(context.Background(), 5)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	if !rows.Next() {
		t.Fatal("want a row")
	}

	var id int64
	if err := rows.Scan(&id); err != nil || id != 5 {
		t.Fatalf("scan: %d %v", id, err)
	}
}

func TestCloseCanFail(t *testing.T) {
	db := Open(Config{CloseErr: errScripted})

	if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	if err := db.Close(); !errors.Is(err, errScripted) {
		t.Fatalf("close: %v", err)
	}
}

func TestColumnsAreReportedToTheCaller(t *testing.T) {
	db := Open(Config{Fallback: Response{Columns: []string{"a", "b"}}})
	defer db.Close()

	rows, err := db.QueryContext(context.Background(), "SELECT a, b FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	if len(columns) != 2 || columns[0] != "a" || columns[1] != "b" {
		t.Fatalf("got %v", columns)
	}
}

func TestPrepareErrsFailsAChosenPrepare(t *testing.T) {
	db := Open(Config{PrepareErrs: []error{nil, errScripted}})
	defer db.Close()

	ctx := context.Background()

	first, err := db.PrepareContext(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("first prepare should succeed: %v", err)
	}
	first.Close()

	if _, err := db.PrepareContext(ctx, "SELECT 2"); !errors.Is(err, errScripted) {
		t.Fatalf("second prepare: %v", err)
	}

	third, err := db.PrepareContext(ctx, "SELECT 3")
	if err != nil {
		t.Fatalf("third prepare should succeed: %v", err)
	}
	third.Close()
}
