.PHONY: fmt vet lint test build clean run docker-up docker-down

# Format code
fmt:
	gofmt -w .

# Find suspicious code
vet:
	go vet ./...

# Run linter (install: brew install golangci-lint)
lint:
	golangci-lint run

# Run all checks
check: fmt vet lint

# Run tests
test:
	go test ./...

# Build binaries
build:
	go build -o bin/chat-service ./cmd/chat
	go build -o bin/user-service ./cmd/user

# Clean build artifacts
clean:
	rm -rf bin/

# Run locally (requires PostgreSQL and Redis)
run:
	go run ./cmd/chat -conf ./configs

# Generate protobuf code
proto:
	protoc --proto_path=. \
		--proto_path=./third_party \
		--go_out=paths=source_relative:. \
		--go-grpc_out=paths=source_relative:. \
		--go-http_out=paths=source_relative:. \
		api/chat/v1/*.proto api/user/v1/*.proto

# Generate Wire dependencies
wire:
	wire ./cmd/chat
	wire ./cmd/user

# Docker commands
docker-up:
	docker-compose up -d

docker-down:
	docker-compose down

docker-build:
	docker-compose up -d --build

docker-logs:
	docker-compose logs -f

# Load test
load-test:
	k6 run test/simple-load-test.js

# Help
help:
	@echo "Available commands:"
	@echo "  make fmt          - Format code"
	@echo "  make vet          - Find suspicious code"
	@echo "  make lint         - Run linter"
	@echo "  make check        - Run all checks (fmt, vet, lint)"
	@echo "  make test         - Run tests"
	@echo "  make build        - Build binaries"
	@echo "  make run          - Run locally"
	@echo "  make docker-up    - Start Docker containers"
	@echo "  make docker-down  - Stop Docker containers"
	@echo "  make load-test    - Run load test"
