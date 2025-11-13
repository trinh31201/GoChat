# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Update dependencies
RUN go mod tidy

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o chat-app ./cmd/chat

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/chat-app .
COPY --from=builder /app/web ./web
COPY --from=builder /app/openapi.yaml ./openapi.yaml
# Note: configs directory is mounted as a volume in docker-compose.yml

# Expose ports
EXPOSE 8000 9000

# Run the application
CMD ["./chat-app", "-conf", "./configs"]