.PHONY: build test lint run docker-build docker-up docker-down clean fmt tidy

# ---------- Build ----------

## Build the server binary.
build:
	go build -ldflags="-s -w" -o bin/lark-server ./cmd/lark-server

# ---------- Test ----------

## Run all tests with race detector.
test:
	go test ./... -race -count=1

# ---------- Lint ----------

## Run golangci-lint.
lint:
	golangci-lint run ./...

# ---------- Run ----------

## Build and run the server locally.
run: build
	./bin/lark-server

# ---------- Docker ----------

## Build the Docker image.
docker-build:
	docker build -t lark-daemon:latest .

## Start services with Docker Compose.
docker-up:
	docker compose up -d

## Stop services.
docker-down:
	docker compose down

# ---------- Utilities ----------

## Format all Go source files.
fmt:
	go fmt ./...

## Tidy module dependencies.
tidy:
	go mod tidy

## Remove build artifacts and runtime data.
clean:
	rm -rf bin/ data/ coverage.out
