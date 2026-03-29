# ======= Этап 1: Сборка =======
FROM golang:1.25.0 AS builder

WORKDIR /build

COPY orderService/go.mod orderService/go.sum ./orderService/
COPY shared/go.mod shared/go.sum ./shared/
COPY protos/go.mod protos/go.sum ./protos/

WORKDIR /build/orderService
RUN --mount=type=cache,target=/go/pkg/mod \
    GOWORK=off go mod download

CWORKDIR /build
 COPY orderService ./orderService
 COPY shared ./shared
 COPY protos ./protos
 COPY config.yaml ./config.yaml

WORKDIR /build/orderService
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off \
    go build -o /out/server ./cmd/order/main.go

FROM ghcr.io/grpc-ecosystem/grpc-health-probe:v0.4.28 AS grpc_health_probe

# ======= Этап 2: Запуск =======
FROM alpine:3.20

WORKDIR /app

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /out/server ./server
COPY --from=builder /build/config.yaml ./config.yaml
COPY --from=grpc_health_probe /ko-app/grpc_health_probe /usr/local/bin/grpc_health_probe

RUN chmod +x /usr/local/bin/grpc_health_probe && \
    chown -R appuser:appgroup /app

USER appuser

EXPOSE 50051

CMD ["./server"]
