FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /chat-service ./cmd/chat

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /chat-service .
COPY --from=builder /app/web ./web

EXPOSE 8000 9000
CMD ["./chat-service"]
