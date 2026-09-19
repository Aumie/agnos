.PHONY: build test vet fmt up down migrate-up migrate-down logs

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

up:
	docker compose -f deployments/docker-compose.yml up --build

down:
	docker compose -f deployments/docker-compose.yml down -v

logs:
	docker compose -f deployments/docker-compose.yml logs -f api

migrate-up:
	docker compose -f deployments/docker-compose.yml run --rm migrate -path /migrations -database "$(DATABASE_URL)" up

migrate-down:
	docker compose -f deployments/docker-compose.yml run --rm migrate -path /migrations -database "$(DATABASE_URL)" down
