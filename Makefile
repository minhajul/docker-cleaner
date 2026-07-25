BINARY_NAME=docker-cleaner

.PHONY: all build run test lint clean fmt fmt-check tidy vuln ci help vet

all: fmt-check vet test build

build:
	go build -o $(BINARY_NAME) main.go

run: build
	./$(BINARY_NAME)

test: ## Run tests with race detector and coverage
	go test -race -coverprofile=coverage.out ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go source files
	gofmt -s -w .

fmt-check: ## Check formatting (CI)
	test -z "$$(gofmt -l .)"

tidy: ## Clean up go.mod and go.sum
	go mod tidy

vuln: ## Run vulnerability scan (requires govulncheck)
	govulncheck ./...

lint: ## Run golangci-lint
	golangci-lint run

ci: fmt-check vet test vuln lint ## Full CI pipeline (no build)

help: ## Show this help message
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

clean:
	go clean
	rm -f $(BINARY_NAME)
