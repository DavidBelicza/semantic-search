.PHONY: install-lancedb test

install-lancedb:
	./scripts/install_lancedb.sh

test: install-lancedb
	go test ./...
