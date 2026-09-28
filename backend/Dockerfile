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
# The server loads .env at startup from CWD/exe-dir; bake it in so the container
# is self-sufficient regardless of where `docker compose up` is invoked from.
COPY .env /root/.env

EXPOSE 8080

CMD ["./server"]