FROM golang:1.25 AS builder

WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .

RUN go build -o redis-server ./cmd

FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/redis-server /usr/local/bin/redis-server

COPY appendonly.aof /app/appendonly.aof

EXPOSE 6379

CMD ["redis-server"]
