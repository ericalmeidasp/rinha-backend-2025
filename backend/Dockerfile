# Build stage
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o app main.go

# Run stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/app .
RUN mkdir -p /tmp /shared
EXPOSE 8080
CMD ["./app"] 