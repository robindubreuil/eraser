.PHONY: build test lint cover clean

build:
	go build ./...

test:
	go test -race ./...

lint: build
	golangci-lint run --timeout 5m ./...

cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo ""
	@echo "Total: $$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}')"

clean:
	rm -f coverage.out
	go clean ./...

all: build test lint
