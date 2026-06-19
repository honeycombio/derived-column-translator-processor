.PHONY: build test lint fmt tidy

# Build the dc2ottl CLI.
build:
	go build -o bin/dc2ottl ./cmd/dc2ottl

# Run all tests in the core module.
test:
	go test ./...

# Report formatting problems without rewriting.
lint:
	gofmt -l .
	go vet ./...

# Rewrite files to gofmt style.
fmt:
	gofmt -w .

tidy:
	go mod tidy
