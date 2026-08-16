.PHONY: run server worker migrate test cover lint docker-up docker-down deps

deps:
	go mod tidy

server:
	go run ./cmd/server

worker:
	go run ./cmd/worker

migrate:
	go run ./cmd/migrate -command up

test:
	go test ./... -count=1

cover:
	go test ./internal/... ./pkg/... -coverprofile=coverage.out -count=1
	go tool cover -func=coverage.out
	@go tool cover -func=coverage.out | grep total

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down -v

lint:
	golangci-lint run ./...
