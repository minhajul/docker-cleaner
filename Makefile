BINARY_NAME=docker-cleaner

.PHONY: all build run test lint clean

all: build

build:
	go build -o $(BINARY_NAME) main.go

run: build
	./$(BINARY_NAME)

test:
	go test ./...

lint:
	go vet ./...

clean:
	go clean
	rm -f $(BINARY_NAME)
