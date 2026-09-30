FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

COPY --from=builder /app/server .

# Runtime config and credentials MUST NOT be baked into the image.
# They come from environment variables / mounted secrets (docker compose env_file,
# Kubernetes Secret + volume).

EXPOSE 8080

CMD ["./server"]