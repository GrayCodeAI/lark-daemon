.PHONY: build run clean test lint

# Build server binary.
build:
	go build -o bin/lark-server ./cmd/lark-server

# Build server only.
build-server:
	go build -o bin/lark-server ./cmd/lark-server

# Run the server.
run: build-server
	./bin/lark-server

# Run with live reload (requires air).
dev:
	air -c .air.toml

# Run tests.
test:
	go test ./... -v -count=1

# Run linter.
lint:
	golangci-lint run ./...

# Clean build artifacts.
clean:
	rm -rf bin/ data/

# Generate docs.
docs:
	@echo "Docs generation not yet implemented"

# Database migrations.
migrate:
	@echo "Migrations run automatically on server start"

# Docker build.
docker:
	docker build -t lark:latest .

# Docker compose up.
up:
	docker compose up -d

# Docker compose down.
down:
	docker compose down
