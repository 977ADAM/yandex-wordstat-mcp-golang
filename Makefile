.PHONY: build test run docker-build docker-run

BINARY := bin/yandex-wordstat-mcp
IMAGE := yandex-wordstat-mcp:local

build:
	@mkdir -p bin
	go build -o $(BINARY) ./cmd/server

test:
	go test ./...

run:
	@go run ./cmd/server

docker-build:
	docker build -t $(IMAGE) .

docker-run:
	@docker compose run --rm -T wordstat-mcp
