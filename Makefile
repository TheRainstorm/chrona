.PHONY: build run test

build:
	go build -trimpath -ldflags="-s -w" -o bin/chrona ./cmd/chrona

run: build
	./bin/chrona -listen 0.0.0.0:8086

test:
	go test -race ./...
