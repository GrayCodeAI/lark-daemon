.PHONY: build test lint run docker-build docker-up docker-down clean fmt tidy npm-publish release-build

# ---------- Build ----------

## Build all binaries.
build:
	go build -ldflags="-s -w" -o bin/lark-server ./cmd/lark-server
	go build -ldflags="-s -w" -o bin/lark-agent ./cmd/lark-agent

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

# ---------- Release ----------

## Build binaries for all platforms.
release-build:
	GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/lark-daemon-linux-amd64  ./cmd/lark-server
	GOOS=linux  GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/lark-daemon-linux-arm64  ./cmd/lark-server
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/lark-daemon-darwin-amd64 ./cmd/lark-server
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/lark-daemon-darwin-arm64 ./cmd/lark-server
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/lark-daemon-windows-amd64.exe ./cmd/lark-server

## Publish the npm wrapper package.
npm-publish:
	cd npm && npm publish
