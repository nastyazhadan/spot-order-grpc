# spotOrder — gRPC Microservices (Go)

Два gRPC-сервиса для работы со спотовыми торговыми инструментами и ордерами.

- **SpotInstrumentService** — справочник рынков (`markets`) с фильтрацией по ролям пользователя, Redis-кэшем и `singleflight`
- **OrderService** — создание ордеров и получение статуса, с проверкой рынка через SpotService, JWT-аутентификацией, circuit breaker и per-user rate limiting

## Технологический стек

Go 1.25 · gRPC / Protobuf / Protovalidate · PostgreSQL 17 · Redis 7 · OpenTelemetry · Prometheus · Grafana · Tempo · Circuit Breaker (`gobreaker`) · JWT · Zap · Goose · Taskfile

---

## Требования

- [Docker](https://docs.docker.com/get-docker/) + Docker Compose
- [Go 1.22+](https://go.dev/dl/)
- [Task](https://taskfile.dev/installation/) _(опционально, для удобных команд)_
- [Postman v10+](https://www.postman.com/downloads/) _(для ручного тестирования)_

---

## Запуск

### 1. Настройка окружения

```bash
cp .env.example .env
```

`.env` уже содержит рабочие значения для локального запуска. При необходимости поменяйте пароли.

### 2. Запуск инфраструктуры

```bash
docker compose up --build -d
```

Docker Compose поднимает весь стек целиком:

| Контейнер | Описание | Порты |
|---|---|---|
| `spot-service` | SpotInstrumentService | `50052` gRPC, `9092` metrics |
| `order-service` | OrderService | `50051` gRPC, `9091` metrics |
| `mypostgres` | PostgreSQL 17 | `5433` |
| `myredis` | Redis 7 | `6379` |
| `otel-collector` | OpenTelemetry Collector | `4317` (OTLP gRPC), `13133` (health), `8888` (collector metrics) |
| `prometheus` | Prometheus | `9090` |
| `grafana` | Grafana | `3000` |
| `tempo` | Tempo | `3200` |
| `pushgateway` | Prometheus Pushgateway | `9093` |

PostgreSQL при старте создаёт базы `order_db` и `spot_db`, а сами сервисы затем автоматически применяют SQL-миграции через Goose. Seed-данные рынков загружаются миграцией `spotService`.

Полезные адреса после запуска:

| Компонент | Адрес |
|---|---|
| OrderService gRPC | `localhost:50051` |
| SpotInstrumentService gRPC | `localhost:50052` |
| OrderService metrics | `http://localhost:9091/metrics` |
| SpotInstrumentService metrics | `http://localhost:9092/metrics` |
| OTel Collector health | `http://localhost:13133/health` |
| Prometheus | `http://localhost:9090` |
| Grafana | `http://localhost:3000` |
| Pushgateway | `http://localhost:9093` |

---

## Конфигурация

Основная конфигурация читается из `config.yaml` (через Viper). Секреты и строки подключения задаются через переменные окружения (`.env`).

### Переменные окружения (`.env`)

| Переменная | Описание |
|---|---|
| `JWT_SECRET` | секрет для подписи и проверки JWT — **обязателен для OrderService** |
| `ORDER_DB_URI` | URI подключения к `order_db` — **обязателен** |
| `SPOT_DB_URI` | URI подключения к `spot_db` — **обязателен** |
| `POSTGRES_USER` | пользователь PostgreSQL для docker-compose |
| `POSTGRES_PASSWORD` | пароль PostgreSQL для docker-compose |
| `POSTGRES_PORT` | внешний порт PostgreSQL (по умолчанию `5433`) |
| `ORDER_DB` | имя базы для OrderService |
| `SPOT_DB` | имя базы для SpotService |
| `EXTERNAL_REDIS_PORT` | внешний порт Redis (по умолчанию `6379`) |
| `ORDER_GRPC_PORT` | внешний gRPC-порт OrderService |
| `SPOT_GRPC_PORT` | внешний gRPC-порт SpotInstrumentService |
| `ORDER_METRICS_PORT` | внешний HTTP-порт метрик OrderService |
| `SPOT_METRICS_PORT` | внешний HTTP-порт метрик SpotInstrumentService |
| `OTEL_GRPC_PORT` | OTLP gRPC-порт OTel Collector (по умолчанию `4317`) |
| `OTEL_HEALTHCHECK_PORT` | health-check порт OTel Collector (по умолчанию `13133`) |
| `PROMETHEUS_METRICS_PORT` | HTTP-порт метрик самого OTel Collector (`8888`) |
| `PROMETHEUS_PORT` | внешний порт Prometheus |
| `PUSHGATEWAY_PORT` | внешний порт Pushgateway |
| `GRAFANA_PORT` | внешний порт Grafana |
| `GRAFANA_ADMIN_USER` | логин администратора Grafana |
| `GRAFANA_ADMIN_PASSWORD` | пароль администратора Grafana |
| `TEMPO_HTTP_PORT` | внешний HTTP-порт Tempo |

### Параметры `config.yaml`

Ниже основные секции. Полный файл — `config.yaml` в корне проекта.

#### `order` — OrderService

| Параметр | Значение по умолчанию | Описание |
|---|---|---|
| `address` | `:50051` | адрес для прослушивания |
| `spot_address` | `spot-service:50052` | адрес SpotService для исходящих вызовов |
| `create_timeout` | `5s` | таймаут на создание ордера |
| `check_timeout` | `15s` | таймаут health check при старте |
| `log_level` | `info` | уровень логов (`debug`, `info`, `warn`, `error`) |
| `log_format` | `console` | формат логов (`console` или `json`) |
| `max_recv_msg_size` | `10485760` | макс. размер входящего gRPC-сообщения (10 MB) |
| `grpc_rate_limit` | `1000` | глобальный RPS-лимит на сервере |
| `circuit_breaker.max_requests` | `3` | макс. запросов в half-open состоянии |
| `circuit_breaker.interval` | `10s` | интервал сброса счётчиков ошибок |
| `circuit_breaker.timeout` | `3s` | время до перехода из open → half-open |
| `circuit_breaker.max_failures` | `5` | порог последовательных отказов для открытия circuit breaker |
| `postgres_pool.max_conns` | `10` | макс. соединений в пуле |
| `postgres_pool.min_conns` | `2` | мин. соединений в пуле |
| `postgres_pool.max_conn_lifetime` | `30m` | макс. lifetime соединения |
| `postgres_pool.max_conn_idle_time` | `5m` | макс. idle time соединения |
| `redis.host` | `myredis` | Redis host |
| `redis.port` | `6379` | Redis port |
| `redis.connection_timeout` | `3s` | таймаут подключения к Redis |
| `redis.pool_size` | `10` | размер пула соединений |
| `redis.min_idle` | `2` | минимум idle-соединений |
| `redis.max_idle` | `10` | максимум idle-соединений |
| `redis.max_active_conns` | `15` | максимум активных соединений |
| `redis.idle_timeout` | `1m` | idle timeout |
| `redis.max_conn_lifetime` | `30m` | max lifetime для соединения |
| `rate_limiter.create_order` | `5` | лимит `CreateOrder` на пользователя за окно |
| `rate_limiter.get_order_status` | `50` | лимит `GetOrderStatus` на пользователя за окно |
| `rate_limiter.window` | `1h` | окно per-user rate limiter |
| `tracing.exporter_otlp_endpoint` | `otel-collector:4317` | адрес OTel Collector |
| `tracing.service_name` | `order-service` | имя сервиса в telemetry |
| `tracing.environment` | `development` | окружение |
| `tracing.service_version` | `1.0.0` | версия сервиса |
| `metrics.http_address` | `:9091` | HTTP-адрес Prometheus metrics endpoint |
| `metrics.export_interval` | `15s` | интервал экспорта OTEL metrics |
| `metrics.push_gateway_url` | `http://pushgateway:9093` | адрес Pushgateway |
| `keep_alive.ping_time` | `20s` | интервал server keepalive ping |
| `keep_alive.ping_timeout` | `5s` | timeout ожидания pong |
| `keep_alive.min_ping_interval` | `10s` | минимальный интервал клиентских ping |
| `keep_alive.permit_without_stream` | `true` | разрешить keepalive без активного стрима |

#### `spot` — SpotInstrumentService

Аналогичные секции `address`, `log_level`, `log_format`, `max_recv_msg_size`, `grpc_rate_limit`, `postgres_pool`, `redis`, `tracing`, `metrics`, `keep_alive`. 
Дополнительно:

| Параметр | Значение по умолчанию | Описание |
|---|---|---|
| `load_markets_timeout` | `5s` | таймаут загрузки рынков |
| `redis.spot_cache_ttl` | `24h` | TTL кэша рынков в Redis |

---

## Трейсинг и метрики (OpenTelemetry + Prometheus + Grafana + Tempo)

Что происходит сейчас:

- трейсы экспортируются в `otel-collector`, а дальше уходят в `Tempo`
- метрики сервисов доступны по HTTP на `:9091/metrics` и `:9092/metrics`
- OTel metrics также экспортируются через collector в Prometheus Remote Write
- Grafana подключена к `Prometheus` и `Tempo` через provisioning
- при shutdown сервисы отправляют служебную метрику в Pushgateway

**Архитектура:**
```
OrderService / SpotService
        ↓ OTLP gRPC (otel-collector:4317)
  otel-collector
    ├── traces  → Tempo
    └── metrics → Prometheus Remote Write

Prometheus ← scrape:
  - order-service:9091
  - spot-service:9092
  - otel-collector:8888
  - pushgateway:9093

Grafana → Prometheus + Tempo
```

Контекст трейса автоматически передаётся между сервисами через gRPC metadata (W3C TraceContext). В логах используется zap-логгер с trace-id из контекста.

**Покрытые операции:**
- `UnaryServerInterceptor` / `UnaryClientInterceptor` — каждый gRPC-вызов
- `order.check_rate_limit`, `order.validate_market`, `order.save_order`, `order.fetch_order`
- `spot.view_markets`, `spot.load_and_warm_cache`, `spot.get_markets_from_repo`
- `postgres.save_order`, `postgres.get_order`, `postgres.list_all_markets`
- `redis.get_markets`, `redis.set_markets`

**Интерфейсы:**
- Grafana UI: `http://localhost:3000`
- Prometheus UI: `http://localhost:9090`

---

## Тестирование в Postman

### Шаг 1 — Создать gRPC-запрос

`NewSpotClient → gRPC Request`

### Шаг 2 — Загрузить .proto файлы

Нажмите **Select a method → Import a .proto file**.

| Файл | Путь в проекте |
|---|---|
| Спецификация OrderService | `shared/protos/proto/order/v1/order.proto` |
| Спецификация SpotService | `shared/protos/proto/spot/v1/spot.proto` |

> Альтернатива: используйте gRPC Server Reflection — оба сервиса регистрируют reflection и gRPC Health Checking, поэтому `grpcurl`, `grpcui` и Postman могут получить схему без ручного импорта proto.

---

### SpotInstrumentService — `localhost:50052`

#### `SpotInstrumentService / ViewMarkets`

Возвращает список рынков, отфильтрованных по роли пользователя.

```json
{
  "user_roles": ["ROLE_USER"]
}
```

**Логика фильтрации:**

| Роль | Видит |
|---|---|
| `ROLE_ADMIN` | Все рынки (включая disabled и удалённые) |
| `ROLE_VIEWER` | Все неудалённые рынки (включая disabled) |
| `ROLE_USER` | Только `enabled: true` и неудалённые |

При передаче нескольких ролей применяется наиболее привилегированная.

**Пример ответа:**
```json
{
  "markets": [
    { "id": "d0cc356d-e52c-4237-9d83-53eb86d98c51", "name": "ADA-USDT", "enabled": false },
    { "id": "89052438-c599-4953-83a0-d26af775a23f", "name": "BTC-USDT", "enabled": true },
    { "id": "6a90185b-6c4a-4b06-b0bf-2aa079c82c6e", "name": "ETH-USDT", "enabled": false },
    { "id": "9d8ccf49-fd84-40c2-b784-2b50553dfcc0", "name": "SOL-USDT", "enabled": true }
  ]
}
```

> Seed-данные: `BTC-USDT`, `ETH-USDT`, `DOGE-USDT`, `SOL-USDT`, `ADA-USDT`.
> `ETH-USDT` и `ADA-USDT` — `enabled: false`, `DOGE-USDT` — удалён (не виден для `ROLE_USER` и `ROLE_VIEWER`).

---

### OrderService — `localhost:50051`

Для вызовов `OrderService` нужен JWT в metadata / headers:

```text
authorization: Bearer <token>
```

Сгенерировать тестовый токен можно из корня проекта:

```bash
task token:generate
```
Текущий генератор создаёт токен с `user_id = 550e8400-e29b-41d4-a716-446655440003`.

#### `OrderService / CreateOrder`

Создаёт ордер. Перед сохранением проверяет, что `market_id` существует и активен в SpotService.

```json
{
  "market_id": "",
  "order_type": "TYPE_LIMIT",
  "price": { "value": "45000.50" },
  "quantity": 2
}
```

| Поле | Тип | Требования                                                        |
|---|---|-------------------------------------------------------------------|
| `market_id` | UUID | обязательно, должен существовать в SpotService                    |
| `order_type` | enum | `TYPE_LIMIT`, `TYPE_MARKET`, `TYPE_STOP_LOSS`, `TYPE_TAKE_PROFIT` |
| `price.value` | string | число > 0                                                         |
| `quantity` | int64 | число > 0                                                         |

> `user_id` больше не передаётся в request — он извлекается из JWT токена unary interceptor-ом.

**Пример ответа:**
```json
{
  "order_id": "7f3b2a90-1c4d-4e5f-8a9b-0c1d2e3f4a5b",
  "status": "STATUS_CREATED"
}
```

> ⚠️ **Rate Limiter:** не более **5 ордеров в час** на один `user_id` из токена. При превышении — `RESOURCE_EXHAUSTED`.

---

#### `OrderService / GetOrderStatus`

Возвращает статус ранее созданного ордера.

```json
{
  "order_id": "",
}
```

**Пример ответа:**
```json
{
  "status": "STATUS_CREATED"
}
```

> Ордер возвращается только если `user_id` из JWT совпадает с создателем — иначе `NOT_FOUND`.
> Rate limit: **50 запросов в час** на `user_id` из токена.

---

### Возможные gRPC-статусы

| Код | Причина |
|---|---|
| `OK` | Успех |
| `INVALID_ARGUMENT` | пустые или некорректные поля, неверный UUID, пустые/дублирующиеся роли |
| `UNAUTHENTICATED` | отсутствует или невалиден JWT для `OrderService` |
| `NOT_FOUND` | Рынок или ордер не найден |
| `ALREADY_EXISTS` | Ордер с таким ID уже существует |
| `RESOURCE_EXHAUSTED` | Сработал per-user Rate Limiter или глобальный RPS-лимит |
| `UNAVAILABLE` | Сработал Circuit Breaker или недоступен зависимый сервис |
| `INTERNAL` | Внутренняя ошибка сервера |

---

## Тесты

```bash
# Юнит-тесты
task test

# Юнит-тесты с проверкой race conditions
task test:race

# Интеграционные тесты (требуется запущенный Docker)
task test:integration

# Все тесты (юнит + интеграционные)
task test:all

# Все тесты с проверкой race conditions
task test:all:race

# Очистить кэш тестов
task clean:cache
```

Если Task не установлен, команды доступны напрямую через `go test`.

---

## Структура проекта

```
spotOrder/
├── orderService/
│   ├── cmd/order/main.go                   # точка входа
│   ├── config/load.go                      # загрузка конфига (viper + env)
│   ├── internal/
│   │   ├── application/
│   │   │   ├── dto/                        # inbound/outbound DTO
│   │   │   ├── order/order_app.go          # сборка fx-приложения
│   │   │   └── order/gen/                  # Wire DI (wire.go, wire_gen.go)
│   │   ├── domain/models/order.go          # доменные модели
│   │   ├── grpc/order/order_handler.go     # gRPC-хэндлеры + извлечение user_id из JWT context
│   │   ├── infrastructure/
│   │   │   ├── postgres/order_store.go     # хранение ордеров
│   │   │   └── redis/order_rate_limiter.go # per-user rate limiter (Lua-скрипт)
│   │   └── services/order/order_service.go # бизнес-логика
│   ├── migrations/                         # SQL-миграции (Goose)
│   └── tests/                              # интеграционные тесты
│
├── spotService/
│   ├── cmd/spot/main.go                    # точка входа
│   ├── config/load.go                      # загрузка конфига (viper + env)
│   ├── internal/
│   │   ├── application/
│   │   │   ├── dto/                        # inbound/outbound DTO
│   │   │   ├── spot/spot_instrument_app.go # сборка fx-приложения
│   │   │   └── spot/gen/                   # Wire DI
│   │   ├── grpc/spot/                      # gRPC-хэндлеры
│   │   ├── infrastructure/
│   │   │   ├── postgres/market_store.go    # чтение рынков из БД
│   │   │   └── redis/market_cache.go       # кэш рынков (TTL, по ролям)
│   │   └── services/spot/market_viewer.go  # бизнес-логика + singleflight
│   ├── migrations/                         # SQL-миграции + init DB scripts
│   └── tests/                              # интеграционные тесты
│
├── shared/
│   ├── client/grpc/                        # gRPC-клиенты между сервисами
│   │   ├── mapper/                         # маппинг ролей и markets
│   │   ├── order_client.go                 # клиент OrderService
│   │   └── spot_client.go                  # клиент SpotService + circuit breaker
│   ├── config/
│   │   ├── load.go                         # godotenv + viper
│   │   └── services.go                     # структуры OrderConfig, SpotConfig
│   ├── infrastructure/
│   │   ├── cache/redis_cache_client.go     # обёртка над go-redis
│   │   ├── db/
│   │   │   ├── postgres.go                 # pgxpool + миграции
│   │   │   └── migrator/                   # Goose-мигратор
│   │   ├── health/health_checker.go        # gRPC Health Check
│   │   └── otel/otel_collector_config.yaml # конфиг OTel Collector
│   │   ├── prometheus/prometheus_config.yml
│   │   └── tempo/tempo.yaml
│   ├── interceptors/
│   │   ├── auth/                           # JWT interceptor + token generator
│   │   ├── errors/                         # gRPC error mapping
│   │   ├── logging/                        # gRPC unary logger interceptor
│   │   ├── metrics/                        # gRPC metrics + OTel meter provider
│   │   ├── rate_limit/                     # глобальный RPS-лимит
│   │   ├── recovery/                       # перехват паник
│   │   ├── tracing/                        # tracing interceptors + propagator
│   │   └── validate/                       # protovalidate interceptor
│   ├── metrics/                            # Prometheus-метрики и shutdown push
│   └── models/                             # общие модели (Market, UserRole, Decimal)
│
├── protos/
│   ├── proto/                              # .proto исходники
│   ├── gen/go/                             # сгенерированный Go-код
│   └── lib/                                # зависимости (buf/validate, google/type)
│
├── grafana/                                # provisioning и dashboards
├── config.yaml                             # основная конфигурация обоих сервисов
├── docker-compose.yml                      # весь локальный стек
├── Taskfile.yaml                           # команды для тестов, генерации кода
├── go.work                                 # Go workspace
└── .env.example                            # шаблон переменных окружения
```

---

## Архитектура

```
  [SpotClient / grpcurl / Postman]
              │ gRPC
              ▼
┌────────────────────────────────────┐
│           OrderService             │  :50051
│  interceptors (chain):             │
│   rateLimiter → tracer → meter →   │
│   logger → recoverer → auth →      │
│   errorMapper → validator          │
│────────────────────────────────────│
│  OrderHandler                      │
│  OrderService (business)           │
│    ├── RateLimitByUser (Redis)         │
│    ├── SpotClient ─────────────────┼──── gRPC ──→ SpotService
│    │      (circuit breaker)        │
│    └── OrderStore (PostgreSQL)     │
└────────────────────────────────────┘

┌────────────────────────────────────┐
│       SpotInstrumentService        │  :50052
│  interceptors (chain):             │
│   rateLimiter → tracer → meter →   │
│   logger → recoverer →             │
│   errorMapper → validator          │
│────────────────────────────────────│
│  SpotInstrumentHandler             │
│  MarketViewer (business)           │
│    ├── MarketCache (Redis)         │
│    │   └── singleflight            │
│    └── MarketStore (PostgreSQL)    │
└────────────────────────────────────┘

Оба сервиса:
  - экспортируют traces и metrics через OTel
  - отдают Prometheus metrics по HTTP
  - регистрируют gRPC reflection и health check
  - наблюдаются через Prometheus + Grafana + Tempo
```
