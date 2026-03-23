# ======= Этап 1: Сборка =======
FROM golang:1.25.0 AS builder

WORKDIR /build/spotOrder

COPY go.work go.work.sum* ./
COPY orderConsumer/go.mod orderConsumer/go.sum ./orderConsumer/
COPY protos/go.mod protos/go.sum ./protos/
COPY shared/go.mod shared/go.sum ./shared/
COPY spotService/go.mod spotService/go.sum ./spotService/

RUN cd orderConsumer && go mod download && \
    cd ../protos && go mod download && \
    cd ../shared && go mod download && \
    cd ../spotService && go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./spotService/cmd/spot/main.go

# ======= Этап 2: Запуск =======
FROM alpine:3.20

WORKDIR /app

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /build/spotOrder/server .
COPY --from=builder /build/spotOrder/config.yaml .

RUN chown -R appuser:appgroup /app

RUN wget -qO /usr/local/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/v0.4.28/grpc_health_probe-linux-amd64 \
    && chmod +x /usr/local/bin/grpc_health_probe

USER appuser

EXPOSE 50052

CMD ["./server"]
