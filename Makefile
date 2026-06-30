.PHONY: install-lancedb test lint

install-lancedb:
	./scripts/install_lancedb.sh

test: install-lancedb
	go test ./...

lint: install-lancedb
	golangci-lint run ./...
