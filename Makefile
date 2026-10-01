.PHONY: build test run docker-build docker-run

BINARY := bin/yandex-wordstat-mcp

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/server

test:
	go test ./...

run:
	@go run ./cmd/server

docker-build:
	docker compose build wordstat-mcp

docker-run:
	@docker compose run --rm -T wordstat-mcp
