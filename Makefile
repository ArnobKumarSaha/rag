.PHONY: build fmt

build:
	go build -o bin/rag .

fmt:
	gofmt -s -w .
