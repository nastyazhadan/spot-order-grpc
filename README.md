# spotOrder — gRPC Microservices (Go)

Два gRPC-сервиса для работы со спотовыми торговыми инструментами и ордерами.

- SpotInstrumentService — справочник рынков (markets) + фильтрация по ролям
- OrderService — создание ордеров и получение статуса (с проверкой рынка в SpotService)

## Требования

- [Docker](https://docs.docker.com/get-docker/) + Docker Compose
- [Go 1.25+](https://go.dev/dl/)
- [Task](https://taskfile.dev/installation/) _(опционально, для удобного запуска команд)_
- [Postman v10+](https://www.postman.com/downloads/)

**Стек:** Go · gRPC / Protobuf · PostgreSQL · Redis · Circuit Breaker · Zap · Goose · Taskfile

---
## Запуск

### 1. Настройка окружения

```bash
cp .env.example .env
```

`.env` уже содержит рабочие значения для локального запуска — при необходимости измените пароли.

### 2. Запуск инфраструктуры (PostgreSQL + Redis)

```bash
docker compose up -d
```

Docker Compose поднимает:
- **PostgreSQL 17** на порту `5433` — автоматически создаёт `order_db` и `spot_db`
- **Redis 7** на порту `6379`

### 3. Запуск сервисов

Из корня в **двух отдельных терминалах**:

```bash
# Терминал 1 — Spot Service
go run ./spotService/cmd/spot/main.go

# Терминал 2 — Order Service
go run ./orderService/cmd/order/main.go
```

Сервисы при старте автоматически накатывают миграции через Goose.

---

## Тестирование в Postman

### Шаг 1 — Создать gRPC-запрос

`New → gRPC Request`

### Шаг 2 — Загрузить .proto файлы

Нажмите **Select a method → Import a .proto file**.

Нужно импортировать два файла и указать директорию с зависимостями:

| Файл | Путь в проекте |
|---|---|
| Спецификация Order Service | `shared/protos/proto/order/v6/order.proto` |
| Спецификация Spot Service | `shared/protos/proto/spot/v6/spot.proto` |

> Можно использовать gRPC Server Reflection — если включить reflection на сервере, инструменты вроде grpcui смогут автоматически подтянуть список сервисов и методов без ручного импорта .proto. Важно подключаться к реальному gRPC-порту сервиса (например, :50051 для OrderService).

### Шаг 3 — Тестирование методов

---

#### SpotInstrument Service — `localhost:50052`

##### `SpotInstrumentService / ViewMarkets`

Возвращает список рынков, отфильтрованных по роли пользователя.

```json
{
  "user_roles": ["ROLE_USER"]
}
```

**Логика фильтрации по ролям:**

| Роль | Видит |
|---|---|
| `ROLE_ADMIN` | Все рынки (включая disabled и удалённые) |
| `ROLE_VIEWER` | Все не удалённые рынки (включая disabled) |
| `ROLE_USER` | Только `enabled: true` и не удалённые |

Можно передать несколько ролей сразу:
```json
{
  "user_roles": ["ROLE_USER", "ROLE_VIEWER"]
}
```

**Пример ответа:**
```json
{
  "markets": [
    {
      "id": "d0cc356d-e52c-4237-9d83-53eb86d98c51",
      "name": "ADA-USDT",
      "enabled": false,
      "deleted_at": null
    },
    {
      "id": "89052438-c599-4953-83a0-d26af775a23f",
      "name": "BTC-USDT",
      "enabled": true,
      "deleted_at": null
    },
    {
      "id": "6a90185b-6c4a-4b06-b0bf-2aa079c82c6e",
      "name": "ETH-USDT",
      "enabled": false,
      "deleted_at": null
    },
    {
      "id": "9d8ccf49-fd84-40c2-b784-2b50553dfcc0",
      "name": "SOL-USDT",
      "enabled": true,
      "deleted_at": null
    }
  ]
}
```

> В базу при старте загружаются seed-данные: `BTC-USDT`, `ETH-USDT`, `DOGE-USDT`, `SOL-USDT`, `ADA-USDT`.
> `ETH-USDT` и `ADA-USDT` — `enabled: false`, `DOGE-USDT` — удалён.

---

#### Order Service — `localhost:50051`

##### `OrderService / CreateOrder`

Создаёт новый ордер. Перед созданием проверяет, что `market_id` существует в SpotService.

```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "market_id": "<uuid из ViewMarkets>",
  "order_type": "TYPE_LIMIT",
  "price": {
    "value": "45000.50"
  },
  "quantity": 2
}
```

| Поле | Тип | Требования |
|---|---|---|
| `user_id` | UUID | обязательно, валидный UUID |
| `market_id` | UUID | обязательно, должен существовать в SpotService |
| `order_type` | enum | `TYPE_LIMIT`, `TYPE_MARKET`, `TYPE_STOP_LOSS`, `TYPE_TAKE_PROFIT` |
| `price.value` | string | число > 0 (передаётся строкой) |
| `quantity` | int64 | > 0 |

**Пример ответа:**
```json
{
  "order_id": "7f3b2a90-1c4d-4e5f-8a9b-0c1d2e3f4a5b",
  "status": "STATUS_CREATED"
}
```

> ⚠️ **Rate Limiter**: не более **5 ордеров в час** на одного `user_id`. При превышении — ответ `RESOURCE_EXHAUSTED`.

---

##### `OrderService / GetOrderStatus`

Проверяет статус ранее созданного ордера.

```json
{
  "order_id": "<uuid из CreateOrder>",
  "user_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

**Пример ответа:**
```json
{
  "status": "STATUS_CREATED"
}
```

> Ордер возвращается только если `user_id` совпадает с тем, кто создавал — иначе `NOT_FOUND`.

Rate limit:

- лимит на чтение — `RATE_LIMIT_GET_ORDER_STATUS` запросов за `RATE_LIMIT_WINDOW`

---

### Возможные gRPC-статусы

| Код | Причина |
|---|---|
| `OK` | Успех |
| `INVALID_ARGUMENT` | Пустые или некорректные поля, неверный UUID |
| `NOT_FOUND` | Рынок или ордер не найден |
| `ALREADY_EXISTS` | Ордер с таким ID уже существует |
| `RESOURCE_EXHAUSTED` | Сработал Rate Limiter |
| `UNAVAILABLE` | Сработал Circuit Breaker (SpotService недоступен) |
| `INTERNAL` | Внутренняя ошибка сервера |

---

## Тесты

Используется [Task](https://taskfile.dev/installation/). Если Task не установлен — команды можно запускать напрямую через `go test`.

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
---

## Структура проекта

```
spotOrder/
├── orderService/
│   ├── cmd/order/main.go           # точка входа
│   ├── internal/
│   │   ├── application             # сборка приложения
│   │   ├── domain/models/          # доменные модели
│   │   ├── grpc/order/             # gRPC-хэндлеры
│   │   ├── infrastructure/
│   │   │   ├── postgres/           # хранение ордеров
│   │   │   └── redis/              # rate limiter
│   │   └── services/order/         # бизнес-логика
│   ├── migrations/                 # SQL-миграции (Goose)
│   └── tests/                      # интеграционные тесты
│
├── spotService/
│   ├── cmd/spot/main.go            # точка входа
│   ├── internal/
│   │   ├── application             # сборка приложения
│   │   ├── grpc/spot/              # gRPC-хэндлеры
│   │   ├── infrastructure/
│   │   │   ├── postgres/           # хранение рынков
│   │   │   └── redis/              # кэш рынков (TTL)
│   │   └── services/spot/          # бизнес-логика + фильтрация по ролям
│   ├── migrations/                 # SQL-миграции (Goose)
│   └── tests/                      # интеграционные тесты
│
├── shared/
│   ├── client/grpc/                # gRPC-клиент (OrderService → SpotService)
│   ├── config/                     # загрузка .env
│   ├── errors/                     # общие ошибки (service, repository)
│   ├── infrastructure/
│   │   ├── db/                     # PostgreSQL + migrator
│   │   ├── health/                 # health checker
│   │   └── redis/                  # Redis-клиент
│   ├── interceptors/               # Zap logger, X-Request-ID, Panic Recovery, Validator
│   └── models/                     # общие модели и маппинг
│
├── protos/
│   ├── proto/                      # исходники .proto
│   ├── gen/go/                     # сгенерированный Go-код
│   └── lib/                        # зависимости (buf/validate, google/type)
│
├── docker-compose.yml              # PostgreSQL + Redis
├── Taskfile.yaml                   # команды для запуска тестов и генерации
├── go.work                         # Go workspace
└── .env.example
```

---

## Конфигурация

Переменные читаются из окружения (удобно через `.env`).

| Переменная | По умолчанию | Описание |
|---|---|---|
| `ORDER_ADDRESS` | `:50051` | Адрес OrderService (listen) |
| `SPOT_INSTRUMENT_LISTEN_ADDRESS` | `:50052` | Адрес SpotService (listen) |
| `SPOT_INSTRUMENT_DIAL_ADDRESS` | `localhost:50052` | Адрес SpotService (dial из OrderService) |
| `ORDER_DB_URI` | — | URI подключения к `order_db` (обязателен) |
| `SPOT_DB_URI` | — | URI подключения к `spot_db` (обязателен) |
| `LOG_LEVEL` | `info` | Уровень логов |
| `LOG_FORMAT` | `console` | Формат логов |
| `CB_MAX_REQUESTS` | `3` | Circuit Breaker max requests (half-open) |
| `CB_INTERVAL` | `10s` | Circuit Breaker interval |
| `CB_TIMEOUT` | `5s` | Circuit Breaker timeout |
| `CB_MAX_FAILURES` | `5` | Порог отказов Circuit Breaker |
| `ORDER_CREATE_TIMEOUT` | `5s` | Таймаут на создание ордера |
| `ORDER_CHECK_TIMEOUT` | `2s` | Таймаут на получение статуса (если используется в сервисе) |
| `GS_TIMEOUT` | `5s` | Таймаут graceful shutdown |
| `REDIS_HOST` | `localhost` | Redis host |
| `REDIS_PORT` | `6379` | Redis port (внутренний) |
| `REDIS_CONNECTION_TIMEOUT` | `10s` | Таймаут подключения Redis |
| `REDIS_MAX_IDLE` | `10` | Max idle connections |
| `REDIS_IDLE_TIMEOUT` | `10s` | Idle timeout |
| `SPOT_REDIS_CACHE_TTL` | `24h` | TTL кэша рынков (SpotService) |
| `RATE_LIMIT_CREATE_ORDER` | `5` | Лимит CreateOrder за окно |
| `RATE_LIMIT_GET_ORDER_STATUS` | `50` | Лимит GetOrderStatus за окно |
| `RATE_LIMIT_WINDOW` | `1h` | Окно Rate Limiter |

Переменные ниже нужны в основном для `docker-compose.yml` (а не для кода напрямую):

- `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_PORT`
- `ORDER_DB`, `SPOT_DB`
- `EXTERNAL_REDIS_PORT`