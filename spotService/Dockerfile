# ======= Этап 1: Сборка =======
FROM golang:1.25.0 AS builder

WORKDIR /build

COPY spotService/go.mod spotService/go.sum ./spotService/
COPY protos/go.mod protos/go.sum ./protos/
COPY shared/go.mod shared/go.sum ./shared/

WORKDIR /build/spotService
RUN --mount=type=cache,target=/go/pkg/mod \
    GOWORK=off go mod download

WORKDIR /build
COPY spotService ./spotService
COPY protos ./protos
COPY shared ./shared
COPY config.yaml ./config.yaml

WORKDIR /build/spotService
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off \
    go build -o /out/server ./cmd/spot/main.go

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOWORK=off \
    go build -o /out/migrate ./cmd/migrate/main.go

FROM ghcr.io/grpc-ecosystem/grpc-health-probe:v0.4.28 AS grpc_health_probe

# ======= Этап 2: Запуск =======
FROM alpine:3.20

WORKDIR /app

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /out/server ./server
COPY --from=builder /out/migrate ./migrate
COPY --from=builder /build/config.yaml ./config.yaml
COPY --from=grpc_health_probe /ko-app/grpc-health-probe /usr/local/bin/grpc_health_probe

RUN chmod +x /usr/local/bin/grpc_health_probe && \
    chown -R appuser:appgroup /app

USER appuser

EXPOSE 50052

CMD ["./server"]
