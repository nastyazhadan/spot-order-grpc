# spotOrder — gRPC Microservices (Go)

Два gRPC-сервиса для работы со спотовыми торговыми инструментами и ордерами.

- **SpotInstrumentService** — справочник рынков (markets) с фильтрацией по ролям пользователя и Redis-кэшом
- **OrderService** — создание ордеров и получение статуса, с проверкой рынка через SpotService, circuit breaker и per-user rate limiting

## Технологический стек

Go · gRPC / Protobuf · PostgreSQL 17 · Redis 7 · OpenTelemetry · Jaeger · Circuit Breaker (gobreaker) · Zap · Goose · Wire · Taskfile

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
docker compose up -d
```

Docker Compose поднимает четыре сервиса:

| Контейнер | Описание | Порт |
|---|---|---|
| `mypostgres` | PostgreSQL 17 | `5433` |
| `myredis` | Redis 7 | `6379` |
| `otel-collector` | OpenTelemetry Collector | `4317` (gRPC), `4318` (HTTP) |
| `jaeger` | Jaeger UI (distributed tracing) | `16686` |

PostgreSQL при старте автоматически создаёт базы `order_db` и `spot_db`, а также загружает seed-данные рынков.

> **Примечание:** сервисы (`orderService` и `spotService`) запускаются **локально**, а не в Docker. Подключение к PostgreSQL и Redis идёт через `localhost`.

### 3. Запуск сервисов

Из корня проекта в **двух отдельных терминалах**:

```bash
# Терминал 1 — SpotInstrumentService (порт :50052)
go run ./spotService/cmd/spot/main.go

# Терминал 2 — OrderService (порт :50051)
go run ./orderService/cmd/order/main.go
```

Сервисы при старте автоматически применяют SQL-миграции через Goose.

---

## Конфигурация

Основная конфигурация читается из `config.yaml` (через Viper). Секреты подключения к БД задаются через переменные окружения (`.env`).

### Переменные окружения (`.env`)

| Переменная | Описание |
|---|---|
| `ORDER_DB_URI` | URI подключения к `order_db` — **обязательна** |
| `SPOT_DB_URI` | URI подключения к `spot_db` — **обязательна** |
| `POSTGRES_USER` | Пользователь PostgreSQL (для docker-compose) |
| `POSTGRES_PASSWORD` | Пароль PostgreSQL (для docker-compose) |
| `POSTGRES_PORT` | Внешний порт PostgreSQL (по умолчанию `5433`) |
| `ORDER_DB` | Имя базы для OrderService |
| `SPOT_DB` | Имя базы для SpotService |
| `EXTERNAL_REDIS_PORT` | Внешний порт Redis (по умолчанию `6379`) |
| `OTEL_GRPC_PORT` | gRPC-порт OTel Collector (по умолчанию `4317`) |
| `OTEL_HEALTH_PORT` | Health-check порт OTel Collector (по умолчанию `13133`) |
| `JAEGER_UI_PORT` | Порт Jaeger UI (по умолчанию `16686`) |

### Параметры `config.yaml`

Ниже основные секции. Полный файл — `config.yaml` в корне проекта.

#### `order` — OrderService

| Параметр | Значение по умолчанию | Описание |
|---|---|---|
| `address` | `:50051` | Адрес для прослушивания |
| `spot_address` | `localhost:50052` | Адрес SpotService (для исходящих вызовов) |
| `create_timeout` | `5s` | Таймаут на создание ордера |
| `check_timeout` | `15s` | Таймаут health check при старте |
| `log_level` | `info` | Уровень логов (`debug`, `info`, `warn`, `error`) |
| `log_format` | `console` | Формат логов (`console` или `json`) |
| `gs_timeout` | `5s` | Таймаут graceful shutdown |
| `max_recv_msg_size` | `10485760` | Макс. размер входящего gRPC-сообщения (10 MB) |
| `grpc_rate_limit` | `1000` | Глобальный RPS-лимит на сервере (token bucket) |
| `circuit_breaker.max_requests` | `3` | Макс. запросов в полуоткрытом состоянии |
| `circuit_breaker.interval` | `10s` | Интервал сброса счётчиков ошибок |
| `circuit_breaker.timeout` | `5s` | Время до перехода из open → half-open |
| `circuit_breaker.max_failures` | `5` | Порог последовательных отказов для открытия |
| `postgres_pool.max_conns` | `10` | Макс. соединений в пуле |
| `postgres_pool.min_conns` | `2` | Мин. соединений в пуле |
| `redis.host` | `localhost` | Redis host |
| `redis.port` | `6379` | Redis port |
| `redis.pool_size` | `10` | Размер пула соединений |
| `rate_limiter.create_order` | `5` | Лимит CreateOrder на пользователя за окно |
| `rate_limiter.get_order_status` | `50` | Лимит GetOrderStatus на пользователя за окно |
| `rate_limiter.window` | `1h` | Окно rate limiter |
| `tracing.exporter_otlp_endpoint` | `localhost:4317` | Адрес OTel Collector |

#### `spot` — SpotInstrumentService

Аналогичные секции `address`, `log_level`, `log_format`, `gs_timeout`, `max_recv_msg_size`, `grpc_rate_limit`, `postgres_pool`, `redis`, `tracing`. Дополнительно:

| Параметр | Значение по умолчанию | Описание |
|---|---|---|
| `redis.spot_cache_ttl` | `24h` | TTL кэша рынков в Redis |

---

## Трейсинг (OpenTelemetry + Jaeger)

Оба сервиса инструментированы через OpenTelemetry. Трейсы экспортируются в Jaeger через OTel Collector по протоколу OTLP gRPC.

**Архитектура:**
```
OrderService / SpotService
        ↓ OTLP gRPC (localhost:4317)
  otel-collector
        ↓ OTLP gRPC (jaeger:4317)
       Jaeger
        ↑ UI (localhost:16686)
```

Контекст трейса автоматически передаётся между сервисами через метаданные gRPC (W3C TraceContext). В каждый лог-запись автоматически добавляется `x-trace-id` активного спана.

**Покрытые операции:**
- `UnaryServerInterceptor` / `UnaryClientInterceptor` — каждый gRPC-вызов
- `order.check_rate_limit`, `order.validate_market`, `order.save_order`, `order.fetch_order`
- `spot.view_markets`, `spot.load_and_warm_cache`, `spot.get_markets_from_repo`
- `postgres.save_order`, `postgres.get_order`, `postgres.list_all_markets`
- `redis.get_markets`, `redis.set_markets`

**Jaeger UI:** http://localhost:16686

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

> Альтернатива: используйте gRPC Server Reflection — сервисы регистрируют рефлексию, поэтому grpcui / grpcurl могут автоматически получить схему без импорта .proto. Подключайтесь к реальному порту сервиса (`:50051` или `:50052`).

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

#### `OrderService / CreateOrder`

Создаёт ордер. Перед сохранением проверяет, что `market_id` существует и активен в SpotService.

```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "market_id": "",
  "order_type": "TYPE_LIMIT",
  "price": { "value": "45000.50" },
  "quantity": 2
}
```

| Поле | Тип | Требования                                                        |
|---|---|-------------------------------------------------------------------|
| `user_id` | UUID | обязательно                                                       |
| `market_id` | UUID | обязательно, должен существовать в SpotService                    |
| `order_type` | enum | `TYPE_LIMIT`, `TYPE_MARKET`, `TYPE_STOP_LOSS`, `TYPE_TAKE_PROFIT` |
| `price.value` | string | число > 0                                                         |
| `quantity` | int64 | число > 0                                                         |

**Пример ответа:**
```json
{
  "order_id": "7f3b2a90-1c4d-4e5f-8a9b-0c1d2e3f4a5b",
  "status": "STATUS_CREATED"
}
```

> ⚠️ **Rate Limiter:** не более **5 ордеров в час** на один `user_id`. При превышении — `RESOURCE_EXHAUSTED`.

---

#### `OrderService / GetOrderStatus`

Возвращает статус ранее созданного ордера.

```json
{
  "order_id": "",
  "user_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Пример ответа:**
```json
{
  "status": "STATUS_CREATED"
}
```

> Ордер возвращается только если `user_id` совпадает с создателем — иначе `NOT_FOUND`.
> Rate limit: **50 запросов в час** на `user_id`.

---

### Возможные gRPC-статусы

| Код | Причина |
|---|---|
| `OK` | Успех |
| `INVALID_ARGUMENT` | Пустые или некорректные поля, неверный UUID |
| `NOT_FOUND` | Рынок или ордер не найден |
| `ALREADY_EXISTS` | Ордер с таким ID уже существует |
| `RESOURCE_EXHAUSTED` | Сработал per-user Rate Limiter или глобальный RPS-лимит |
| `UNAVAILABLE` | Сработал Circuit Breaker (SpotService недоступен) |
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

Если Task не установлен, команды доступны напрямую через `go test`:

```bash
# Юнит-тесты
go test ./orderService/... ./spotService/... -v

# Интеграционные тесты
go test -tags=integration ./orderService/tests/... -v -timeout 10m
go test -tags=integration ./spotService/tests/... -v -timeout 5m
```

---

## Структура проекта

```
spotOrder/
├── orderService/
│   ├── cmd/order/main.go                   # точка входа
│   ├── config/load.go                      # загрузка конфига (viper + env)
│   ├── internal/
│   │   ├── application/
│   │   │   ├── order/order_app.go          # сборка fx-приложения
│   │   │   └── gen/                        # Wire DI (wire.go, wire_gen.go)
│   │   ├── domain/models/order.go          # доменные модели
│   │   ├── grpc/order/order_handler.go     # gRPC-хэндлеры + маппинг ошибок
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
│   │   │   ├── spot/spot_instrument_app.go # сборка fx-приложения
│   │   │   └── gen/                        # Wire DI
│   │   ├── grpc/spot/                      # gRPC-хэндлеры
│   │   ├── infrastructure/
│   │   │   ├── postgres/market_store.go    # чтение рынков из БД
│   │   │   └── redis/market_cache.go       # кэш рынков (TTL, по ролям)
│   │   └── services/spot/market_viewer.go  # бизнес-логика + singleflight
│   ├── migrations/                         # SQL-миграции + seed-данные
│   └── tests/                              # интеграционные тесты
│
├── shared/
│   ├── client/grpc/                        # gRPC-клиент OrderService → SpotService
│   │   └── client_grpc.go                  # circuit breaker + маппинг
│   ├── config/
│   │   ├── load.go                         # godotenv + viper
│   │   └── services.go                     # структуры OrderConfig, SpotConfig
│   ├── errors/
│   │   ├── repository/errors.go            # ошибки слоя репозитория
│   │   └── service/errors.go               # ошибки слоя сервиса
│   ├── infrastructure/
│   │   ├── cache/redis_cache_client.go     # обёртка над go-redis
│   │   ├── db/
│   │   │   ├── postgres.go                 # pgxpool + миграции
│   │   │   └── migrator/                   # Goose-мигратор
│   │   ├── health/health_checker.go        # gRPC Health Check
│   │   └── otel/otel_collector_config.yaml # конфиг OTel Collector
│   ├── interceptors/
│   │   ├── logger/logger.go                # gRPC unary logger interceptor
│   │   ├── logger/zap/zap_logger.go        # zap + trace-id из контекста
│   │   ├── rate_limit/grpc_rate_limiter.go # глобальный RPS-лимит (token bucket)
│   │   ├── recovery/panic_recovery.go      # перехват паник
│   │   ├── tracing/
│   │   │   ├── tracer.go                   # init/shutdown TracerProvider
│   │   │   ├── grpc_interceptor.go         # server/client tracing interceptors
│   │   │   └── metadata_carrier.go         # W3C propagation через gRPC metadata
│   │   └── validate/proto_validator.go     # protovalidate interceptor
│   └── models/                             # общие модели (Market, UserRole, Decimal)
│
├── protos/
│   ├── proto/                              # .proto исходники
│   ├── gen/go/                             # сгенерированный Go-код
│   └── lib/                                # зависимости (buf/validate, google/type)
│
├── config.yaml                             # основная конфигурация обоих сервисов
├── docker-compose.yml                      # PostgreSQL + Redis + OTel Collector + Jaeger
├── Taskfile.yaml                           # команды для тестов, генерации кода
├── go.work                                 # Go workspace
└── .env.example                            # шаблон переменных окружения
```

---

## Архитектура

```
  [SpotClient]
      │ gRPC
      ▼
┌─────────────────────────────┐
│        OrderService         │  :50051
│  interceptors (chain):      │
│   rateLimiter → tracer →    │
│   logger → recoverer →      │
│   validator                 │
│─────────────────────────────│
│  OrderHandler               │
│  OrderService (business)    │
│    ├── RateLimiter (Redis)  │
│    ├── MarketViewer ────────┼──── gRPC ──→ SpotService
│    └── OrderStore (PG)      │              (circuit breaker)
└─────────────────────────────┘

┌─────────────────────────────┐
│       SpotService           │  :50052
│  (те же interceptors)       │
│─────────────────────────────│
│  SpotInstrumentHandler      │
│  MarketViewer (business)    │
│    ├── MarketCache (Redis)  │
│    │   └── singleflight     │
│    └── MarketStore (PG)     │
└─────────────────────────────┘

Оба сервиса → OTel Collector → Jaeger
```
