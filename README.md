# spotOrder — gRPC Microservices (Go)

Два gRPC-сервиса для работы со спотовыми торговыми инструментами и ордерами.

- **SpotInstrumentService** — справочник рынков (`markets`) с фильтрацией по ролям пользователя, Redis-кэшем и `singleflight`. Поллер отслеживает изменения рынков, записывает события в outbox и после обработки обновлений инициирует refresh role-specific Redis-кэша
- **OrderService** — создание ордеров и получение статуса, с проверкой рынка через SpotService, JWT-аутентификацией, circuit breaker и per-user rate limiting. При получении события `market.state.changed` компенсирует активные ордера заблокированного рынка.

## Быстрый старт

```bash
cp .env.example .env
docker compose up --build -d
```

После запуска:
- OrderService gRPC: `localhost:50051`
- SpotInstrumentService gRPC: `localhost:50052`
- Grafana: `http://localhost:3000`
- Kafka UI: `http://localhost:8090`

---

## Технологический стек
- Go 1.25 workspace (`go.work`)
- gRPC + Protobuf + Protovalidate
- PostgreSQL 17
- Redis 7
- Kafka
- OpenTelemetry Collector + Tempo
- Prometheus + Grafana
- FX для сборки приложения
- Goose migrations
- Taskfile для локальных команд

## Состав сервисов

### SpotInstrumentService

gRPC-методы:
- `ViewMarkets`
- `GetMarketByID`

Что делает:
- читает рынки из `market_store`
- фильтрует выдачу по ролям
- кеширует списки рынков в Redis
- использует `singleflight` при промахе кеша
- запускает `MarketPoller`, который отслеживает изменения в БД и складывает события в outbox
- публикует `market.state.changed` в Kafka

### OrderService

gRPC-методы:
- `CreateOrder`
- `GetOrderStatus`
- `RefreshToken`

Что делает:
- создаёт ордера в PostgreSQL
- валидирует рынок через `SpotInstrumentService`
- использует JWT-аутентификацию для пользовательских методов
- применяет per-user rate limit через Redis
- хранит блокировки рынков в Redis
- пишет события в outbox и публикует их в Kafka
- читает `market.state.changed` из Kafka и запускает компенсационную обработку

## Требования

- [Docker](https://docs.docker.com/get-docker/) + Docker Compose
- [Go 1.22+](https://go.dev/dl/)
- [Task](https://taskfile.dev/installation/) _(опционально, для удобных команд)_
- Postman / grpcurl / grpcui — опционально для ручной проверки

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
| `order-service` | gRPC + metrics | `50051`, `9091` |
| `spot-service` | gRPC + metrics | `50052`, `9093` |
| `mypostgres` | PostgreSQL | `5433` |
| `myredis` | Redis | `6379` |
| `kafka` | Kafka broker | `9092` |
| `kafka-ui` | UI для Kafka | `8090` |
| `otel-collector` | OTLP collector | `4317`, `13133`, `8888` |
| `prometheus` | Prometheus | `9090` |
| `grafana` | Grafana | `3000` |
| `tempo` | Tempo | `3200` |

PostgreSQL при старте создаёт базы `order_db` и `spot_db`, а сами сервисы затем автоматически применяют SQL-миграции через Goose. Seed-данные рынков загружаются миграцией `spotService`.

Полезные адреса после запуска:

| Компонент | Адрес |
|---|---|
| OrderService gRPC | `localhost:50051` |
| SpotInstrumentService gRPC | `localhost:50052` |
| OrderService metrics | `http://localhost:9091/metrics` |
| SpotInstrumentService metrics | `http://localhost:9093/metrics` |
| Kafka UI | `http://localhost:8090` |
| OTel Collector health | `http://localhost:13133/health` |
| Prometheus | `http://localhost:9090` |
| Grafana | `http://localhost:3000` |
| Tempo | `http://localhost:3200` |

---

## Конфигурация

Проект использует:
- общий `config.yaml` в корне
- `.env` / переменные окружения для строк подключения и части runtime-настроек

### Обязательные переменные окружения

Для локального запуска фактически обязательны:

- `ORDER_DB_URI`
- `SPOT_DB_URI`
- `JWT_SECRET`

Остальные значения в `.env` нужны в основном для `docker-compose` и внешних портов.

### Структура `config.yaml`

Конфиг разделён на две верхнеуровневые секции: `order` и `spot`.

#### Секция `order`

| Подсекция | Ключевые параметры |
|---|---|
| `service` | `address`, `name`, `max_recv_msg_size` |
| `spot_address` | адрес SpotService |
| `timeouts` | `service`, `check` |
| `auth` | `skip_methods`, `access_token_ttl`, `refresh_token_ttl` |
| `circuit_breaker` | `max_requests`, `interval`, `timeout`, `max_failures` |
| `rate_limit_by_user` | `create_order`, `get_order_status`, `window` |
| `kafka` | `brokers`, `producer`, `consumer`, `topics`, `outbox` |
| `postgres_pool`, `redis`, `grpc_rate_limit`, `tracing`, `metrics`, `keep_alive`, `retry` | — |

Значения по умолчанию:
- `service.address = :50051`
- `spot_address = spot-service:50052`
- `timeouts.service = 5s`, `timeouts.check = 15s`
- `metrics.http_address = :9091`
- `rate_limit_by_user.create_order = 5`, `rate_limit_by_user.get_order_status = 50`

#### Секция `spot`

| Подсекция | Ключевые параметры |
|---|---|
| `service` | `address`, `name`, `max_recv_msg_size` |
| `timeouts` | `service` |
| `redis` | `spot_cache_ttl` |
| `market_poller` | `poll_interval`, `processing_timeout`, `batch_size` |
| `kafka` | `brokers`, `producer`, `topics`, `outbox` |
| `postgres_pool`, `grpc_rate_limit`, `tracing`, `metrics`, `keep_alive` | — |

Значения по умолчанию:
- `service.address = :50052`
- `timeouts.service = 5s`
- `redis.spot_cache_ttl = 5m`
- `metrics.http_address = :9093`
- `market_poller.poll_interval = 1s`, `processing_timeout = 5s`, `batch_size = 100`

---

## Трейсинг и метрики (OpenTelemetry + Prometheus + Grafana + Tempo)

Что происходит сейчас:

- трейсы экспортируются в `otel-collector`, а дальше уходят в `Tempo`
- метрики сервисов доступны по HTTP на `:9091/metrics` и `:9093/metrics`
- OTel metrics также экспортируются через collector в Prometheus Remote Write
- Grafana подключена к `Prometheus` и `Tempo` через provisioning

**Архитектура:**
```
OrderService / SpotService
        ↓ OTLP gRPC (otel-collector:4317)
  otel-collector
    ├── traces  → Tempo
    └── metrics → Prometheus Remote Write

Prometheus ← scrape:
  - order-service:9091
  - spot-service:9093
  - otel-collector:8888

Grafana → Prometheus + Tempo
```

Контекст трейса автоматически передаётся между сервисами через gRPC metadata (W3C TraceContext). В логах используется zap-логгер с trace-id из контекста.

**Покрытые операции:**
- `UnaryServerInterceptor` / `UnaryClientInterceptor` — каждый gRPC-вызов
- `order.check_rate_limit`, `order.validate_market`, `order.save_order`, `order.fetch_order`
- `spot.view_markets`, `spot.load_and_warm_cache`, `spot.get_markets_from_repo`
- `postgres.save_order`, `postgres.get_order`, `postgres.list_all_markets`, `postgres.list_updated_since`
- `redis.get_markets`, `redis.set_markets`
- `market_compensation.process` — обработка события `market.state.changed` в OrderService
- `producer.produce_market_state_changed_batch` — публикация батча событий рынков в SpotService

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
| Спецификация OrderService | `protos/proto/order/v1/order.proto` |
| Спецификация SpotService | `protos/proto/spot/v1/spot.proto` |

> Альтернатива: используйте gRPC Server Reflection — оба сервиса регистрируют reflection и gRPC Health Checking, поэтому `grpcurl`, `grpcui` и Postman могут получить схему без ручного импорта proto.

---

### SpotInstrumentService — `localhost:50052`

#### `ViewMarkets`

Возвращает список рынков, отфильтрованных по роли пользователя.

```json
{
  "limit": 100,
  "offset": 0
}
```
> `user_roles` больше не передаются в request — они извлекается из JWT токена unary interceptor-ом.

**Логика фильтрации:**

| Роль | Видит |
|---|---|
| `ROLE_ADMIN` | Все рынки (включая disabled и удалённые) |
| `ROLE_VIEWER` | Все неудалённые рынки (включая disabled) |
| `ROLE_USER` | Только `enabled: true` и неудалённые |

Если в JWT несколько ролей, применяется наиболее привилегированная роль.

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
#### `GetMarketByID`

```json
{
  "market_id": "<uuid>"
}
```

> Seed-данные: `BTC-USDT`, `ETH-USDT`, `DOGE-USDT`, `SOL-USDT`, `ADA-USDT`.
> `ETH-USDT` и `ADA-USDT` — `enabled: false`, `DOGE-USDT` — удалён (не виден для `ROLE_USER` и `ROLE_VIEWER`).

---

### OrderService — `localhost:50051`

Для вызовов `OrderService` и `Spot Service` нужен JWT в metadata / headers:

```text
authorization: Bearer <token>
```

### Сгенерировать тестовые токены

Из корня проекта:

```bash
task token:gen
```

Что делает helper:
- печатает `access_token`
- печатает `refresh_token`
- сохраняет refresh token в Redis
- использует `user_id = 550e8400-e29b-41d4-a716-446655440003`

Важно: helper использует короткие TTL для dev-проверки:
- access token: `5m`
- refresh token: `10m`

#### `CreateOrder`

Создаёт ордер. Перед сохранением проверяет, что `market_id` существует, активен в SpotService и не заблокирован (не получен сигнал об отключении рынка).

```json
{
  "market_id": "<uuid>",
  "order_type": "TYPE_LIMIT",
  "price": { "value": "45000.50" },
  "quantity": 2
}
```

| Поле | Тип | Требования |
|---|---|---|
| `market_id` | UUID | обязательно, должен существовать в SpotService |
| `order_type` | enum | `TYPE_LIMIT`, `TYPE_MARKET`, `TYPE_STOP_LOSS`, `TYPE_TAKE_PROFIT` |
| `price.value` | string | число > 0, не более 10 целых цифр и 8 знаков после запятой (NUMERIC(18,8)) |
| `quantity` | int64 | число > 0 |

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

#### `GetOrderStatus`

Возвращает статус ранее созданного ордера.

```json
{
  "order_id": "<uuid>"
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

#### `RefreshToken`

```json
{
  "refresh_token": "<token>"
}
```

---

### Возможные gRPC-статусы

| Код | Причина                                                                                         |
|---|-------------------------------------------------------------------------------------------------|
| `OK` | Успешный вызов                                                                                  |
| `INVALID_ARGUMENT` | пустые или некорректные поля, неверный UUID                            |
| `UNAUTHENTICATED` | отсутствует или невалиден JWT                                                |
| `NOT_FOUND` | Рынок или ордер не найден                                                                       |
| `ALREADY_EXISTS` | Ордер с таким ID уже существует                                                                 |
| `FAILED_PRECONDITION` | Рынок отключён (`enabled = false`) |
| `RESOURCE_EXHAUSTED` | Сработал per-user Rate Limiter или глобальный RPS-лимит                                         |
| `UNAVAILABLE` | Сработал Circuit Breaker или недоступен зависимый сервис                                        |
| `INTERNAL` | Внутренняя ошибка сервера                                                                       |

---

## Тесты

Из корня проекта доступны команды:

```bash
task test                    # unit-тесты
task test:short              # без долгих тестов
task test:race               # с детектором гонок
task test:integration        # интеграционные тесты
task test:integration:race   # интеграционные с гонками
task test:all                # все тесты
task test:all:race           # все тесты с гонками
task lint                    # линтер
task fmt                     # форматирование
task tidy                    # go mod tidy
task clean:cache             # очистка кэша
task token:gen               # генерация тестовых токенов
```

---

## Структура проекта

```
spotOrder/
├── orderService/
│   ├── cmd/
│   │   ├── dev/gen_token_helper.go         # хелпер для создания токена
│   │   └── order/main.go                   # точка входа
│   ├── config/load.go                      # загрузка конфига (viper + env)
│   ├── internal/
│   │   ├── application/
│   │   │   ├── dto/                        # inbound/outbound DTO
│   │   │   ├── order_app.go                # точка сборки fx-приложения
│   │   │   └── order/                      # fx DI (providers, lifecycle)
│   │   ├── domain/models/                  # доменные модели
│   │   ├── grpc/                           # gRPC-хэндлеры + извлечение user_id из JWT context
│   │   ├── infrastructure/
│   │   │   ├── postgres/order_store.go     # хранение ордеров
│   │   │   ├── postgres/outbox_store.go    # Transactional Outbox
│   │   │   ├── postgres/inbox_store.go     # Inbox (дедупликация входящих событий)
│   │   │   ├── kafka/outbox_worker.go      # воркер публикации событий из outbox
│   │   │   ├── redis/order_rate_limiter.go # per-user rate limiter (Lua-скрипт)
│   │   │   └── redis/market_block_store.go # хранение блокировок рынков
│   │   └── services/
│   │       ├── order/order_service.go      # бизнес-логика создания ордеров
│   │       ├── order/compensation_service.go # компенсация ордеров при отключении рынка
│   │       └── consumer/market_consumer.go # Kafka-потребитель market.state.changed
│   ├── migrations/                         # SQL-миграции (Goose)
│   └── tests/                              # интеграционные тесты
│
├── spotService/
│   ├── cmd/spot/main.go                    # точка входа
│   ├── config/load.go                      # загрузка конфига (viper + env)
│   ├── internal/
│   │   ├── application/
│   │   │   ├── dto/                        # inbound/outbound DTO
│   │   │   ├── spot_instrument_app.go      # точка сборки fx-приложения
│   │   │   └── spot/                       # fx DI (providers, lifecycle)
│   │   ├── domain/models/                  # доменные модели (cursor, events)
│   │   ├── grpc/spot/                      # gRPC-хэндлеры
│   │   ├── infrastructure/
│   │   │   ├── postgres/market_store.go    # чтение рынков из БД
│   │   │   ├── postgres/cursor_store.go    # курсор поллера (позиция чтения)
│   │   │   ├── postgres/outbox_store.go    # Transactional Outbox
│   │   │   ├── kafka/outbox_worker.go      # воркер публикации событий из outbox
│   │   │   └── redis/market_cache.go       # кэш рынков (TTL, по ролям, инвалидация)
│   │   └── services/
│   │       ├── spot/market_viewer.go       # бизнес-логика + singleflight
│   │       ├── spot/market_poller.go       # поллер изменений рынков (cursor-based)
│   │       └── producer/market_producer.go # outbox-продюсер + инвалидация кэша
│   ├── migrations/                         # SQL-миграции + init DB scripts
│   └── tests/                              # интеграционные тесты
│
├── shared/
│   ├── client/grpc/                        # gRPC-клиенты между сервисами
│   │   ├── breaker/circuit_breaker.go      # gobreaker-обёртка
│   │   ├── mapper/                         # маппинг ролей и markets
│   │   ├── order_client.go                 # клиент OrderService
│   │   └── spot_client.go                  # клиент SpotService + circuit breaker
│   ├── config/
│   │   ├── load.go                         # godotenv + viper
│   │   └── services.go                     # структуры OrderConfig, SpotConfig
│   ├── errors/
│   │   ├── shared_errors.go                # базовые типы ошибок
│   │   ├── service/errors.go               # сервисные ошибки (включая ErrMarketUnavailable)
│   │   └── repository/errors.go            # ошибки репозитория
│   ├── infrastructure/
│   │   ├── cache/redis_cache_client.go     # обёртка над go-redis
│   │   ├── db/
│   │   │   ├── postgres.go                 # pgxpool + миграции
│   │   │   └── migrator/                   # Goose-мигратор
│   │   ├── health/health_checker.go        # gRPC Health Check
│   │   ├── kafka/
│   │   │   ├── consumer/                   # Kafka consumer group + middleware
│   │   │   └── producer/                   # Kafka sync producer
│   │   ├── otel/otel_collector_config.yaml # конфиг OTel Collector
│   │   ├── prometheus/prometheus_config.yml
│   │   └── tempo/tempo.yaml
│   ├── interceptors/
│   │   ├── auth/                           # JWT interceptor + token generator
│   │   ├── errors/                         # gRPC error mapping
│   │   ├── logging/                        # gRPC unary logger interceptor
│   │   ├── metrics/                        # gRPC metrics + OTel meter provider
│   │   ├── ratelimit/                      # глобальный RPS-лимит
│   │   ├── recovery/                       # перехват паник
│   │   ├── tracing/                        # tracing interceptors + propagator
│   │   └── validate/                       # protovalidate interceptor
│   ├── metrics/                            # Prometheus-метрики и shutdown
│   └── models/                             # общие модели (Market, UserRole, Decimal)
│
├── protos/
│   ├── proto/                              # .proto исходники
│   ├── gen/go/                             # сгенерированный Go-код
│   └── lib/                                # зависимости (buf/validate, google/type)
│
├── config.yaml                             # основная конфигурация обоих сервисов
├── docker-compose.yml                      # весь локальный стек
├── Taskfile.yaml                           # команды для тестов, генерации кода
├── go.work                                 # Go workspace
└── .env.example                            # шаблон переменных окружения
```

---

## Архитектура

```
  [grpcurl / Postman / клиент]
              │ gRPC
              ▼
┌────────────────────────────────────────┐
│            OrderService                │  :50051
│  interceptors (chain):                 │
│   recoverer → tracer → meter →         │
│   logger → auth → rateLimiter →        │
│   errorMapper → validator              │
│────────────────────────────────────────│
│  OrderHandler                          │
│  OrderService (business)               │
│    ├── RateLimitByUser (Redis)         │
│    ├── MarketBlockStore (Redis)        │  ← блокировка рынков
│    ├── SpotClient ─────────────────────┼──── gRPC ──→ SpotService
│    │      (circuit breaker + retry)    │
│    └── OrderStore (PostgreSQL)         │
│                                        │
│  CompensationService                   │
│    ├── InboxStore (PostgreSQL)         │  ← дедупликация событий
│    ├── OrderStore (PostgreSQL)         │  ← отмена активных ордеров
│    ├── MarketBlockStore (Redis)        │  ← синхронизация блокировки
│    └── EventProducer → Outbox          │
│                                        │
│  Kafka Consumer ← market.state.changed │
│  Outbox Worker  → order.created        │
│                   order.status.updated │
└────────────────────────────────────────┘

┌────────────────────────────────────────┐
│        SpotInstrumentService           │  :50052
│  interceptors (chain):                 │
│   recoverer → tracer → meter →         │
│   logger → auth → rateLimiter →        │
│   errorMapper → validator              │
│────────────────────────────────────────│
│  SpotInstrumentHandler                 │
│  MarketViewer (business)               │
│    ├── MarketCache (Redis)             │  ← TTL-кэш, инвалидируется при изменениях
│    │   └── singleflight                │
│    └── MarketStore (PostgreSQL)        │
│                                        │
│  MarketPoller (cursor-based)           │
│    └── MarketProducer                  │
│          ├── OutboxStore (PostgreSQL)  │  ← атомарно с курсором
│          └── MarketCache.RefreshAll    │  ← refresh role-based cache после обработки изменений
│                                        │
│  Outbox Worker → market.state.changed  │
└────────────────────────────────────────┘

Поток события при изменении рынка:
  SpotService DB → MarketPoller → Outbox → Kafka
  → OrderService Consumer → CompensationService
    ├── InboxStore (дедупликация)
    ├── CancelActiveOrdersByMarket
    ├── OrderStatusUpdated → Outbox → Kafka
    └── MarketBlockStore.Block/Unblock (Redis)

Оба сервиса:
  - экспортируют traces и metrics через OTel
  - отдают Prometheus metrics по HTTP
  - регистрируют gRPC reflection и health check
  - наблюдаются через Prometheus + Grafana + Tempo
```
