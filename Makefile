.PHONY: build test run image

build:
	go build -o bin/aisphere-sandbox ./cmd/sandbox-manager

test:
	go test ./...

run:
	go run ./cmd/sandbox-manager --config configs/config.json.example

image:
	docker build -t registry.local/aisphere/aisphere-sandbox:latest .
