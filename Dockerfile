FROM golang:1.24-alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o insighta-backend ./cmd/server

# ---
FROM alpine:3.21
RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=builder /app/insighta-backend .

EXPOSE 8080
CMD ["./insighta-backend"]
