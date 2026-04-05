# Техническая спецификация: spotOrder

**Версия:** 1.0  
**Дата:** Апрель 2026  
**Репозиторий:** `github.com/nastyazhadan/spot-order-grpc`

---

## Содержание

1. [Глоссарий](#1-глоссарий)
2. [Контракты сервисных интерфейсов](#2-контракты-сервисных-интерфейсов)
3. [Модель ошибок](#3-модель-ошибок)
4. [Цепочки gRPC-перехватчиков](#4-цепочки-grpc-перехватчиков)
5. [Аутентификация: механика JWT-перехватчика](#5-аутентификация-механика-jwt-перехватчика)
6. [Rate Limiting: реализация и хранение состояния](#6-rate-limiting-реализация-и-хранение-состояния)
7. [MarketBlockStore: протокол синхронизации блокировок](#7-marketblockstore-протокол-синхронизации-блокировок)
8. [Валидация рынка: двухступенчатая проверка](#8-валидация-рынка-двухступенчатая-проверка)
9. [Компенсационный сервис: конечный автомат](#9-компенсационный-сервис-конечный-автомат)
10. [Kafka Consumer: пайплайн middleware](#10-kafka-consumer-пайплайн-middleware)
11. [Transactional Outbox: контракт воркера](#11-transactional-outbox-контракт-воркера)
12. [MarketPoller: cursor-based опрос](#12-marketpoller-cursor-based-опрос)
13. [Prometheus-метрики: полный реестр](#13-prometheus-метрики-полный-реестр)
14. [Redis: схема ключей и форматы значений](#14-redis-схема-ключей-и-форматы-значений)
15. [Схема базы данных: детальная спецификация](#15-схема-базы-данных-детальная-спецификация)
16. [Зависимости между компонентами](#16-зависимости-между-компонентами)

---

## 1. Глоссарий

| Термин | Определение |
|---|---|
| **Market** | Торговая пара (например, BTC-USDT). Хранится в SpotService. |
| **Order** | Торговый ордер пользователя. Хранится в OrderService. |
| **Outbox** | Паттерн атомарной публикации событий через таблицу в БД. |
| **Inbox** | Таблица для гарантированной однократной обработки входящих событий. |
| **Market Block** | Временная блокировка рынка в Redis, запрещающая создание новых ордеров. |
| **Compensation** | Отмена активных ордеров при получении сигнала о недоступности рынка. |
| **Circuit Breaker** | Автоматический размыкатель цепи при превышении порога ошибок к зависимому сервису. |
| **Singleflight** | Схлопывание параллельных запросов с одним ключом в один I/O-запрос. |
| **Cursor** | Позиция поллера: `(updated_at, id)` последней обработанной записи. |
| **DLQ** | Dead Letter Queue — топик для сообщений, которые не удалось обработать после всех повторов. |

---

## 2. Контракты сервисных интерфейсов

Ниже приведены Go-интерфейсы, которые определяют границы между слоями. Реализации этих интерфейсов могут меняться независимо.

### OrderService (бизнес-логика)

```go
// Saver — сохранение ордера в хранилище (PostgreSQL)
type Saver interface {
    SaveOrder(ctx context.Context, tx pgx.Tx, order models.Order) error
}

// Getter — чтение ордера по ID с проверкой владельца
type Getter interface {
    GetOrder(ctx context.Context, id, userID uuid.UUID) (models.Order, error)
}

// MarketViewer — получение рынка через SpotService (gRPC-клиент)
type MarketViewer interface {
    GetMarketByID(ctx context.Context, id uuid.UUID) (sharedModels.Market, error)
}

// MarketBlockStore — управление блокировками рынков в Redis
type MarketBlockStore interface {
    // SyncState обновляет состояние блокировки только если newUpdatedAt >= текущего.
    // Возвращает true, если состояние было фактически обновлено.
    SyncState(ctx context.Context, marketID uuid.UUID, blocked bool, updatedAt time.Time) (bool, error)
    // IsBlocked проверяет наличие блокировки. При cache miss возвращает (false, nil).
    IsBlocked(ctx context.Context, marketID uuid.UUID) (bool, error)
}

// RateLimiter — per-user ограничение частоты запросов
type RateLimiter interface {
    Allow(ctx context.Context, userID uuid.UUID) (bool, error)
    Limit() int64
    Window() time.Duration
}

// EventProducer — запись событий ордеров в Outbox (в рамках транзакции)
type EventProducer interface {
    ProduceOrderCreated(ctx context.Context, tx pgx.Tx, event models.OrderCreatedEvent) error
    ProduceOrderStatusUpdated(ctx context.Context, tx pgx.Tx, event models.OrderStatusUpdatedEvent) error
}

// TransactionManager — управление транзакциями PostgreSQL
type TransactionManager interface {
    Begin(ctx context.Context) (pgx.Tx, error)
}
```

### CompensationService

```go
// MarketInboxWriter — управление состоянием входящих событий (дедупликация)
type MarketInboxWriter interface {
    // BeginProcessing атомарно вставляет запись inbox или возвращает (false, currentStatus, nil)
    // при дубликате. Вставка выполняется в переданной транзакции.
    BeginProcessing(ctx context.Context, tx pgx.Tx, event models.InboxEvent) (bool, models.InboxEventStatus, error)
    MarkProcessed(ctx context.Context, tx pgx.Tx, eventID uuid.UUID, consumerGroup string) error
    // SaveFailed сохраняет запись об ошибке вне транзакции (использует новый контекст).
    SaveFailed(ctx context.Context, event models.InboxEvent, errText string) error
}

// MarketOrderCanceler — отмена активных ордеров по рынку
type MarketOrderCanceler interface {
    // CancelActiveOrdersByMarket переводит все ордера со статусом CREATED/PENDING
    // в статус CANCELLED. Возвращает список отменённых order_id.
    CancelActiveOrdersByMarket(ctx context.Context, tx pgx.Tx, marketID uuid.UUID) ([]uuid.UUID, error)
}
```

### SpotService (бизнес-логика)

```go
// MarketCacheRepository — role-based head-cache первой страницы
type MarketCacheRepository interface {
    GetAll(ctx context.Context, roleKey string) ([]sharedModels.Market, error)
    SetAll(ctx context.Context, markets []sharedModels.Market, roleKey string, ttl time.Duration) error
    DeleteAll(ctx context.Context, roleKey string) error
}

// MarketByIDCacheRepository — кэш рынка по market_id
type MarketByIDCacheRepository interface {
    Get(ctx context.Context, id uuid.UUID) (sharedModels.Market, error)
    Set(ctx context.Context, market sharedModels.Market, ttl time.Duration) error
    Delete(ctx context.Context, id uuid.UUID) error
}

// MarketReader — чтение рынков с поддержкой cursor-based пагинации
type MarketReader interface {
    // ListUpdatedSince возвращает рынки, изменённые после since с курсором (afterID).
    // Результат упорядочен по (updated_at ASC, id ASC).
    ListUpdatedSince(ctx context.Context, since time.Time, afterID uuid.UUID, limit int) ([]sharedModels.Market, error)
}

// MarketEventProducer — публикация батча событий через Outbox
type MarketEventProducer interface {
    // PublishMarketStateChanged записывает события в outbox и сохраняет курсор атомарно.
    // Обновление Redis-кэша выполняется отдельно логикой MarketPoller после успешной обработки батча.
    PublishMarketStateChanged(ctx context.Context, events []sharedModels.MarketStateChangedEvent, cursor models.PollerCursor) error
}

// CursorStore — чтение позиции поллера
type CursorStore interface {
    Get(ctx context.Context, pollerName string) (models.PollerCursor, error)
}
```

---

## 3. Модель ошибок

### Иерархия внутренних ошибок

```
shared/errors/
├── ErrNotFound{ID}          — универсальный "не найден"
├── ErrAlreadyExists{ID}     — дубликат записи
├── ErrMarketNotFound{ID}    — рынок не найден
└── ErrCacheNotFound         — промах кэша (sentinel)

shared/errors/service/
├── ErrLimitExceeded{Limit, Window}  — per-user rate limit
├── ErrUnavailable{ID}               — рынок временно недоступен (circuit breaker / kafka event)
├── ErrDisabled{ID}                  — рынок отключён (enabled=false)
├── ErrMarketsNotFound               — рынки не найдены (список пуст)
├── ErrMarketsUnavailable            — список рынков временно недоступен
├── ErrUserRoleNotSpecified          — роль не передана в запросе
├── ErrInvalidSubject                — невалидный sub в JWT
├── ErrInvalidJTI                    — невалидный jti refresh token
├── ErrTokenRevoked                  — refresh token отозван или не найден
├── ErrRevokeTokenFailed             — ошибка отзыва токена в Redis
└── ErrSaveTokenFailed               — ошибка сохранения токена в Redis

shared/errors/repository/
└── ErrOrderNotFound, ErrOrderAlreadyExists, ErrMarketsNotFound, ErrMarketCacheCorrupted
```

### Ошибки cache-слоя SpotService

Во внутреннем cache/repository слое SpotService отдельно различаются:

- `ErrMarketsNotFound` — role-based head-cache пуст или role-key отсутствует
- `ErrMarketNotFound` — by-id cache пуст или ключ `market:by_id:<marketID>` отсутствует
- `ErrMarketCacheCorrupted` — данные в Redis найдены, но не могут быть корректно десериализованы/смэпплены в доменную модель

`ErrMarketCacheCorrupted` не является публичной бизнес-ошибкой API. Эта ошибка используется для выбора специального fallback-пути:
- для `ViewMarkets` — чтение первой страницы из PostgreSQL и последующий warmup role-based head-cache
- для `GetMarketByID` — повторная загрузка через `singleflight`, попытка warmup by-id cache и, при необходимости, cleanup stale key

### Маппинг ошибок → gRPC-коды

Маппинг выполняется в `shared/interceptors/errors/grpc_error_interceptor.go`. Логика:

1. `context.Canceled` / `context.DeadlineExceeded` → стандартный `status.FromContextError`
2. Уже обёрнутые gRPC-статусы с кодом, отличным от `Unknown`, пропускаются без изменений.
3. Остальные ошибки сопоставляются по таблице:

| Внутренняя ошибка | gRPC-код | Сообщение | Уровень лога |
|---|---|---|---|
| `ErrMarketsNotFound`, `ErrMarketNotFound`, `ErrOrderNotFound` | `NOT_FOUND` | `"resource not found"` | WARN |
| `ErrUnavailable` (circuit breaker / рынок) | `UNAVAILABLE` | `"market temporarily unavailable"` | WARN |
| `ErrMarketsUnavailable` | `UNAVAILABLE` | `err.Error()` | WARN |
| `ErrOrderAlreadyExists` | `ALREADY_EXISTS` | `"order already exists"` | WARN |
| `ErrLimitExceeded` | `RESOURCE_EXHAUSTED` | `err.Error()` (с лимитом и окном) | WARN |
| `ErrUserRoleNotSpecified` | `UNAUTHENTICATED` | `err.Error()` | WARN |
| `ErrInvalidSubject`, `ErrInvalidJTI`, `ErrTokenRevoked` | `UNAUTHENTICATED` | `"refresh token error"` | WARN |
| `gobreaker.ErrOpenState`, `ErrTooManyRequests` | `UNAVAILABLE` | `"service temporarily unavailable"` | — |
| `ErrDisabled` | `FAILED_PRECONDITION` | `"market is disabled"` | WARN |
| `ErrRevokeTokenFailed`, `ErrSaveTokenFailed` | `INTERNAL` | `"internal error"` | ERROR |
| Прочие | `INTERNAL` | `"internal error"` | ERROR |

> **Важно:** Сообщения `NOT_FOUND` и `ALREADY_EXISTS` намеренно не раскрывают внутренние детали. Только `ErrLimitExceeded` возвращает клиенту конкретные значения лимита и окна.

---

## 4. Цепочки gRPC-перехватчиков

Перехватчики применяются в указанном порядке (каждый следующий оборачивает предыдущий). Порядок критичен: паника восстанавливается до трейсинга, аутентификация — после сбора метрик, валидация — последней.

### OrderService

```
recoverer → tracer → meter → logger → auth → rateLimiter → errorMapper → validator
```

| Перехватчик | Пакет | Действие                                            |
|---|---|-----------------------------------------------------|
| `recoverer` | `interceptors/recovery` | Перехватывает `panic`, возвращает `INTERNAL`        |
| `tracer` | `interceptors/tracing` | Создаёт span, инжектирует W3C TraceContext          |
| `meter` | `interceptors/metrics` | Счётчики, in-flight gauge, histogram длительности   |
| `logger` | `interceptors/logging/zap` | Логирует метод, статус, длительность с trace_id     |
| `auth` | `interceptors/auth` | Парсит JWT, кладёт user_id и roles в контекст       |
| `rateLimiter` | `interceptors/ratelimit` | Per-instance RPS-лимит (токен-бакет)                |
| `errorMapper` | `interceptors/errors` | Переводит доменные ошибки в gRPC-статусы            |
| `validator` | `interceptors/validate` | Protovalidate: проверяет поля запроса по аннотациям |

### SpotInstrumentService

```
recoverer → tracer → meter → logger → auth → rateLimiter → errorMapper → validator
```

SpotService тоже использует JWT auth interceptor.

## gRPC Health Checking

Для health-check используется стандартная реализация `google.golang.org/grpc/health`.

Что это означает:
- поддерживается стандартный gRPC Health Checking Protocol
- доступны оба метода: `Check` и `Watch`
- статусы могут выставляться как для пустого service name (`""`), так и для конкретных gRPC-сервисов
- при остановке приложения вызывается `healthServer.Shutdown()`, чтобы watchers получили корректный переход в `NOT_SERVING`

Используемые service names:
- `order.v1.OrderService`
- `auth.v1.AuthService`
- `spot.v1.SpotInstrumentService`

Текущее ограничение:
- на текущем этапе health отражает состояние запущенного процесса и регистрации сервисов
- dependency-aware readiness (с учётом Postgres / Redis / Kafka / downstream) не реализован

---

## 5. Аутентификация: механика JWT-перехватчика

### Алгоритм обработки входящего запроса

```
1. Метод в списке skip_methods? → пропустить перехватчик
2. Извлечь заголовок "authorization" из gRPC metadata
3. Проверить префикс "bearer " (case-insensitive)
4. Распарсить токен: jwt.ParseToken(tokenString, TokenTypeAccess)
   - проверить подпись (HS256, JWT_SECRET)
   - проверить exp
   - проверить token_type claim == "access"
5. Извлечь user_id из sub (UUID), roles из claims.UserRoles
6. Положить userID и roles в контекст через requestctx
7. Передать управление следующему обработчику
```

> **Важно:** JWT-перехватчик **не** проверяет активность сессии в Redis. Это намеренно: проверка сессии (`IsSessionActive`) выполняется только в `AuthService.Refresh` при ротации токенов. Access-токен считается валидным до истечения `exp` — это стандартная stateless-семантика JWT.

### Skip-методы по умолчанию

Методы, не требующие JWT, различаются по сервисам.

Для `order.auth.skip_methods`:
- `/grpc.health.v1.Health/Check`
- `/grpc.health.v1.Health/Watch`
- `/auth.v1.AuthService/RefreshToken`

Для `spot.auth.skip_methods`:
- `/grpc.health.v1.Health/Check`
- `/grpc.health.v1.Health/Watch`

### Структура Claims

```go
type Claims struct {
    jwt.RegisteredClaims                            // sub (user_id), exp, jti
    TokenType TokenType `json:"token_type"`         // "access" | "refresh"
    SessionID string    `json:"session_id"`         // идентификатор сессии (UUID)
    UserRoles []string  `json:"user_roles,omitempty"` // больше не передаются в Spot request
}
```

### Хранение refresh-токенов и сессий в Redis

Система использует **два ключа** для управления refresh-токенами:

| Ключ | Значение | TTL |
|---|---|---|
| `refresh:<userID>:<jti>` | `"1"` (маркер существования токена) | `refresh_token_ttl` |
| `auth_session:<userID>` | sessionID (string, UUID) | `refresh_token_ttl` |

- **Ротация** (при `RefreshToken`): атомарным Lua-скриптом проверяется `auth_session:<userID>` == oldSessionID, удаляется старый `refresh:<userID>:<oldJTI>`, создаётся `refresh:<userID>:<newJTI>` и обновляется `auth_session:<userID>` = newSessionID.
- **Замена** (при первой выдаче токена): создаются оба ключа атомарно.
- **Валидация сессии** (в `AuthService.Refresh`): `IsSessionActive` сверяет `auth_session:<userID>` с `sessionID` из claims refresh-токена.

---

## 6. Rate Limiting: реализация и хранение состояния

### Алгоритм (Lua-скрипт, sliding counter)

Скрипт выполняется атомарно на стороне Redis, что исключает race condition между `INCR` и установкой TTL:

```lua
local count = redis.call('INCR', KEYS[1])
if count == 1 then
    redis.call('PEXPIRE', KEYS[1], ARGV[1])   -- TTL в миллисекундах
end
return count
```

**Семантика:** при первом обращении ключ создаётся и получает TTL, равный длине окна. Счётчик сбрасывается автоматически по истечении TTL. Это скользящий счётчик начала периода, а не точное скользящее окно.

### Redis-ключи rate limiter

| Операция | Ключ | Пример |
|---|---|---|
| CreateOrder | `rate:order:create:<userID>` | `rate:order:create:550e8400-...` |
| GetOrderStatus | `rate:order:get:<userID>` | `rate:order:get:550e8400-...` |

### Лимиты по умолчанию

| Операция | Лимит | Окно |
|---|---|---|
| `CreateOrder` | 5 | 1 час |
| `GetOrderStatus` | 50 | 1 час |

При превышении возвращается `ErrLimitExceeded{Limit: N, Window: W}` → gRPC `RESOURCE_EXHAUSTED`.  
Метрика: `grpc_server_rate_limit_rejected_business_total{service, operation}`.

---

## 7. MarketBlockStore: протокол синхронизации блокировок

### Формат значения в Redis

```
<unix_timestamp_ms>:<state>
```

Примеры: `1743710400000:1` (заблокирован), `1743710400000:0` (разблокирован).

**Ключ:** `market:block:<marketID>`

### Lua-скрипт CAS (Compare-And-Swap)

Атомарно обновляет состояние только если новый timestamp ≥ текущего. Гарантирует монотонность при конкурентных обновлениях (доставка из Kafka + recheck из OrderService):

```lua
local current = redis.call("GET", key)
if not current then
    redis.call("SET", key, ts..":"..state, "PX", ttl)
    return 1   -- обновлено
end

local currentTs = tonumber(string.sub(current, 1, sep-1))
if newTs < currentTs then
    return 0   -- устаревшее обновление, игнорировать
end

redis.call("SET", key, ts..":"..state, "PX", ttl)
return 1   -- обновлено
```

### Обработка повреждённого состояния

Lua-скрипт возвращает ошибку в двух случаях:
- `"invalid market block state"` — значение не содержит разделителя `:`
- `"invalid market block timestamp"` — временна́я метка не парсится как число

В обоих случаях:
1. Ключ удаляется (`DEL market:block:<marketID>`).
2. Метод возвращает ошибку вызывающей стороне.

При `IsBlocked`: аналогично, corrupted state удаляется и возвращается ошибка.

### Поведение при отказе Redis

`IsBlocked` возвращает ошибку при любом сбое (кроме промаха кэша, который даёт `false, nil`). Fallback-логика реализована в приватном методе `OrderService.getMarketBlockedState`, который вызывает `IsBlocked`:

- Если ошибка — `context.Canceled` / `context.DeadlineExceeded` → пробрасывается вызывающей стороне.
- Любая другая ошибка Redis:
    - Логирует предупреждение.
    - Инкрементирует `grpc_server_cache_fallbacks_total{service, operation="market_is_blocked", reason="lookup_error"}`.
    - Возвращает `blocked=false` → система допускает запрос и полагается на последующую проверку через SpotService.

---

## 8. Валидация рынка: двухступенчатая проверка

`OrderService.validateMarket` выполняет двойную проверку, чтобы минимизировать как ложные отказы, так и некорректное разрешение:

```
validateMarket(marketID):
  1. blockStore.IsBlocked(marketID)
     → ошибка Redis? → fallback: blocked=false, продолжить
     → результат: blocked bool

  2. spotClient.GetMarketByID(marketID)  [через circuit breaker + retry]
     → ошибка? →
        если blocked=true → логировать "failing closed"
        вернуть ошибку SpotService

  3. market.DeletedAt != nil?
     → syncMarketBlock(blocked=true, reason="warm_block_after_deleted_recheck")
     → вернуть ErrMarketNotFound

  4. !market.Enabled?
     → syncMarketBlock(blocked=true, reason="warm_block_after_disabled_recheck")
     → вернуть ErrDisabled

  5. blocked=true, но рынок доступен?
     → syncMarketBlock(blocked=false, reason="remove_stale_block_after_recheck")
     → разрешить создание ордера
```

**Смысл двойной проверки:** Redis-состояние блокировки может быть устаревшим (рынок снова включён, но блокировка ещё не снята). Вызов SpotService является авторитетным источником истины.

`syncMarketBlock` вызывается _без ошибки для вызывающей стороны_ — это фоновая синхронизация. Результат фиксируется в метрике `grpc_server_market_block_state_sync_total`.

---

## 9. Компенсационный сервис: конечный автомат

### Состояния Inbox-записи

```
                    BeginProcessing
                         │
              ┌──────────┴──────────┐
              │ новое событие        │ дубликат
              ▼                     ▼
           PENDING            (пропустить)
              │                     │
     ┌────────┴────────┐            └──→ trySyncMarketBlockState
     │ успех           │ ошибка
     ▼                 ▼
  PROCESSED          FAILED
```

### Алгоритм `ProcessMarketStateChanged`

```
ProcessMarketStateChanged(topic, consumerGroup, rawPayload, event):

  BEGIN TRANSACTION

  1. inboxStore.BeginProcessing(tx, inboxEvent)
     → duplicate? → Commit + trySyncMarketBlockState → return nil
     → error?    → rollback + SaveFailed (no tx) → return error

  2. applyCompensationTransaction(tx, event):
     → event.Enabled=true AND event.DeletedAt=nil?
        → ничего не делать (рынок снова доступен)
     → иначе:
        a. CancelActiveOrdersByMarket(tx, marketID)
           → возвращает []cancelledOrderIDs
        b. для каждого orderID:
           ProduceOrderStatusUpdated(tx, {
               OrderID: orderID,
               NewStatus: CANCELLED,
               Reason: "market became unavailable",
               CorrelationID: event.EventID,
           })

  3. inboxStore.MarkProcessed(tx, event.EventID, consumerGroup)

  COMMIT

  4. trySyncMarketBlockState (вне транзакции, новый контекст с timeout)
     blocked = !event.Enabled || event.DeletedAt != nil
     blockStore.SyncState(marketID, blocked, event.UpdatedAt)
```

### Обработка дубликатов

При получении события с уже известным `(event_id, consumer_group)`:
- Статус `PROCESSED`: коммит + синхронизация блокировки (idempotent).
- Статус `PENDING`: коммит + синхронизация блокировки (параллельная обработка).
- Логируется с уровнем INFO, метрики не инкрементируются как ошибки.

### Отказоустойчивость `trySyncMarketBlockState`

Вызывается в отдельном контексте с `context.WithoutCancel` + собственным timeout. Ошибка синхронизации блокировки не прерывает обработку события — она логируется и фиксируется в метрике `grpc_server_market_block_state_sync_total`.

---

## 10. Kafka Consumer: пайплайн middleware

Middleware применяются конструктором через `applyMiddlewares` (обход в обратном порядке), образуя цепочку:

```
PanicRecovery → [DLQ] → [RetryMiddleware] → handler
```

`PanicRecovery` всегда первый — prepend-ится конструктором автоматически. `DLQ` (если включён) регистрируется следующим и оборачивает `Retry`, за счёт чего перехватывает `RetryExhaustedError`.

### PanicRecoveryMiddleware

Перехватывает `panic` в обработчике, логирует с stack trace, возвращает ошибку вместо крэша.

### RetryMiddleware

```
Параметры: maxRetries int, backoff time.Duration
Стратегия: backoff * (attempt + 1) — линейное увеличение
При исчерпании попыток: возвращает RetryExhaustedError{Err, RetryCount}
```

### DLQMiddleware

Оборачивает `Retry`. Перехватывает `RetryExhaustedError` или другие ошибки (кроме `ErrMessageHandledByDLQ`).  
Публикует сообщение в DLQ-топик, добавляя к оригинальным заголовкам три дополнительных:
- `dlq-original-topic` — топик, из которого получено сообщение
- `dlq-retry-count` — количество совершённых попыток
- `dlq-error` — текст последней ошибки (может быть усечён для вписывания в лимит размера)

Partition и offset передаются как поля сообщения, а не как заголовки. При превышении `DLQMaxMessageBytes` поле `dlq-error` и/или payload усекаются, добавляются служебные заголовки `dlq-truncated`, `dlq-original-size`, `dlq-final-size`.

### Управление сессией Consumer Group

Внутренний цикл перезапускает сессию при `ErrRestartConsumerSession` (например, после Kafka rebalance). Внешний цикл в `lifecycle.go` перезапускает весь consumer при фатальных ошибках.

---

## 11. Transactional Outbox: контракт воркера

### Жизненный цикл записи

```
pending → processing → published
                    ↘ failed
```

### Алгоритм poll-итерации

```
FOR EACH batch (locked_by=worker_id, status='pending', available_at <= NOW()):
  UPDATE status='processing', locked_at=NOW()

  FOR EACH record:
    publish to Kafka (synchronous)
    → success: UPDATE status='published', published_at=NOW()
    → error:
        retry_count++
        available_at = NOW() + backoff(retry_count)
        если retry_count >= max_retries: status='failed'
        иначе: status='pending'
```

### Защита от зависших записей

Записи в статусе `processing` с `locked_at` старше `processing_timeout` переводятся обратно в `pending`. Это обрабатывается индексом `idx_outbox_processing_locked_at`.

### Метрики

| Метрика | Лейблы |
|---|---|
| `grpc_server_outbox_events_total` | `service`, `event_type`, `result` |
| `grpc_server_outbox_worker_iteration_duration_seconds` | `service` |

---

## 12. MarketPoller: cursor-based опрос

### Курсор

```go
type PollerCursor struct {
    LastSeenAt time.Time
    LastSeenID uuid.UUID
}
```

Курсор хранится в таблице `market_poller_cursor`. Имя поллера: `market_state_changed_poller`.
`MarketPoller` отслеживает изменения в таблице рынков и синхронизирует связанный state приложения.

### Алгоритм

```
- при старте `Init()` считывает последний сохранённый cursor
- далее `poll()` выбирает очередной батч изменений после этого cursor
- для найденных изменений формируются и сохраняются outbox-события
- после успешной обработки батча новый cursor сохраняется в хранилище
- если изменения действительно были, сервис инициирует `RefreshAll` для role-based head-cache рынков

Важно:

- `Init()` не вызывает `RefreshAll()`
- refresh кэша выполняется как часть логики poller-а после обработки изменений
- outbox worker не инвалидирует Redis-кэш самостоятельно
```

### Атомарность курсора и событий

Запись событий в outbox и обновление курсора выполняются в одной PostgreSQL-транзакции. При откате транзакции курсор не смещается — события будут обработаны повторно (at-least-once).

### Инвалидация кэша

После `COMMIT` poller вызывает `MarketCache.RefreshAll`, чтобы перепрогреть role-based Redis head-cache актуальными данными.
By-id cache (`market:by_id:<marketID>`) не перепрогревается poller-ом и обновляется лениво при `GetMarketByID` либо истекает по TTL.
---

## 13. Prometheus-метрики: полный реестр

Все метрики объявлены в пакете `shared/metrics`. Экспортируются по HTTP на `:9091` (OrderService) и `:9093` (SpotService).

### gRPC

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_requests_total` | Counter | `service`, `method`, `status` | Количество запросов по статусу |
| `grpc_server_request_duration_seconds` | Histogram | `service`, `method` | Латентность обработчика (buckets: 1ms–5s) |
| `grpc_server_in_flight_requests` | Gauge | `service`, `method` | Активные запросы в данный момент |

### Бизнес-метрики

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_orders_created_total` | Counter | `service`, `market_id` | Успешно созданные ордера |
| `grpc_server_rate_limit_rejected_grpc_total` | Counter | `service`, `method` | Отказы глобального RPS-лимита |
| `grpc_server_rate_limit_rejected_business_total` | Counter | `service`, `operation` | Отказы per-user rate limiter |
| `grpc_server_market_block_state_sync_total` | Counter | `service`, `reason`, `blocked`, `result`, `updated` | Попытки синхронизации блокировок рынков |

### Cache (Redis)

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_cache_hits_total` | Counter | `service`, `operation` | Попадания в кэш |
| `grpc_server_cache_misses_total` | Counter | `service`, `operation` | Промахи кэша |
| `grpc_server_cache_operation_duration_seconds` | Histogram | `service`, `operation` | Латентность операций Redis (buckets: 0.1ms–100ms) |
| `grpc_server_cache_fallbacks_total` | Counter | `service`, `operation`, `reason` | Fallback при ошибке Redis |
| `grpc_server_cache_invalidations_total` | Counter | `service`, `reason`, `scope`, `result` | Инвалидации кэша (`role` или `market_by_id`) |
| `grpc_server_cache_warmups_total` | Counter | `service`, `operation`, `scope`, `result` | Прогревы кэша (`role` или `market_by_id`) |

### Database (PostgreSQL)

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_db_query_duration_seconds` | Histogram | `service`, `operation` | Латентность запросов к БД (buckets: 1ms–1s) |

### Circuit Breaker

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_circuit_breaker_state_changes_total` | Counter | `name`, `from`, `to` | Переходы состояний breaker |
| `grpc_server_circuit_breaker_open_total` | Counter | `name` | Сколько раз breaker открывался |

### Kafka

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_kafka_messages_published_total` | Counter | `service`, `topic` | Успешно опубликованные сообщения |
| `grpc_server_kafka_publish_errors_total` | Counter | `service`, `topic` | Ошибки публикации |
| `grpc_server_kafka_publish_duration_seconds` | Histogram | `service`, `topic` | Латентность публикации (buckets: 1ms–1s) |
| `grpc_server_kafka_messages_consumed_total` | Counter | `service`, `topic`, `result` | Потреблённые сообщения (success/error) |
| `grpc_server_kafka_consume_duration_seconds` | Histogram | `service`, `topic` | Время обработки сообщения (buckets: 1ms–5s) |

### Outbox Worker

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_outbox_events_total` | Counter | `service`, `event_type`, `result` | Обработанные события outbox |
| `grpc_server_outbox_worker_iteration_duration_seconds` | Histogram | `service` | Длительность одной итерации воркера |

### Прочее

| Метрика | Тип | Лейблы | Описание |
|---|---|---|---|
| `grpc_server_shutdowns_total` | Counter | `service`, `reason` | Завершения работы сервиса |

### Exemplars

Метрики типа Histogram поддерживают Prometheus Exemplars: при наличии активного trace_id в контексте к наблюдению добавляется лейбл `traceID`. Это позволяет переходить из Grafana-графика напрямую в Tempo по конкретному запросу.

---

## 14. Redis: схема ключей и форматы значений

### OrderService

| Назначение | Ключ | Формат значения | TTL |
|---|---|---|---|
| Rate limit (CreateOrder) | `rate:order:create:<userID>` | integer (counter) | window (1h) |
| Rate limit (GetOrderStatus) | `rate:order:get:<userID>` | integer (counter) | window (1h) |
| Блокировка рынка | `market:block:<marketID>` | `<unix_ms>:<0\|1>` | настраивается |
| Refresh token (маркер) | `refresh:<userID>:<jti>` | `"1"` | refresh_token_ttl |
| Активная сессия | `auth_session:<userID>` | sessionID (string) | refresh_token_ttl |

### SpotService

| Назначение | Ключ | Формат значения | TTL |
|---|---|---|---|
| Head-cache рынков (по роли) | `market:cache:<role>` | JSON ([]Market) | spot_cache_ttl (5m) |
| Кэш рынка по ID | `market:by_id:<marketID>` | JSON (Market) | spot_cache_ttl (5m) |

### Поведение кэшарынков SpotService

`MarketViewer` использует два независимых Redis-кэша:

- role-based head-cache списков рынков по ключам `market:cache:<role>`
- by-id cache рынков по ключам `market:by_id:<marketID>`

### Role-based head-cache (`ViewMarkets`)

`ViewMarkets` использует offset-based pagination. Для ускорения чтения первая страница (`offset = 0`) может обслуживаться из role-based head-cache; все остальные страницы читаются напрямую из PostgreSQL.

Поведение при чтении:
- head-cache используется только для `ViewMarkets`, когда `offset == 0` и `limit <= cacheLimit`
- при `cache hit` ответ формируется из Redis
- при `cache miss` или `corrupted` payload сервис загружает первую страницу из PostgreSQL через `ListPage`, возвращает результат клиенту и выполняет warmup role-based head-cache
- `RefreshAll` перепрогревает role-based head-cache после обработки изменений рынков

Role-based head-cache не использует `singleflight`: конкурентные запросы первой страницы могут независимо сделать fallback в PostgreSQL. Кэш затем прогревается результатом первого успешного чтения или плановым `RefreshAll`.

### By-id cache (`GetMarketByID`)

`GetMarketByID` использует отдельный by-id cache (`market:by_id:<marketID>`). При конкурентном `cache miss` загрузка из PostgreSQL дедуплицируется через `singleflight`, после чего результат прогревает Redis.

Поведение при чтении:
- при `cache hit` рынок возвращается из Redis
- при `cache miss` используется `singleflight`, чтобы только один конкурентный запрос сходил в PostgreSQL и прогрел by-id cache
- при повреждённом (`corrupted`) payload выполняется повторная попытка загрузки через `singleflight`; если прогрев не удался, сервис старается удалить stale key
- после получения рынка из кэша или PostgreSQL ролевые ограничения (`admin/viewer/user`) применяются на уровне `MarketViewer`
---

## 15. Схема базы данных: детальная спецификация

### order_db

#### orders

```sql
CREATE TABLE orders (
    id         UUID           PRIMARY KEY,
    user_id    UUID           NOT NULL,
    market_id  UUID           NOT NULL,
    type       SMALLINT       NOT NULL,  -- OrderType enum: 1=LIMIT 2=MARKET 3=STOP_LOSS 4=TAKE_PROFIT
    price      NUMERIC(18, 8) NOT NULL,
    quantity   BIGINT         NOT NULL,
    status     SMALLINT       NOT NULL,  -- OrderStatus enum: 1=CREATED 2=PENDING 3=FILLED 4=CANCELLED
    created_at TIMESTAMPTZ    NOT NULL,

    CONSTRAINT chk_orders_price_positive    CHECK (price > 0),
    CONSTRAINT chk_orders_quantity_positive CHECK (quantity > 0),
    CONSTRAINT chk_orders_type_valid        CHECK (type    BETWEEN 1 AND 4),
    CONSTRAINT chk_orders_status_valid      CHECK (status  BETWEEN 1 AND 4)
);

CREATE INDEX idx_orders_market_id          ON orders (market_id);
CREATE INDEX idx_orders_user_id_created_at ON orders (user_id, created_at DESC);
```

#### outbox (OrderService)

```sql
CREATE TABLE outbox (
    id           UUID PRIMARY KEY,
    event_id     UUID        NOT NULL,  -- уникальный идентификатор события
    event_type   TEXT        NOT NULL,  -- "order.created" | "order.status.updated"
    aggregate_id UUID        NOT NULL,  -- order_id
    payload      BYTEA       NOT NULL,  -- Protobuf-сериализованное событие
    status       TEXT        NOT NULL DEFAULT 'pending',
    retry_count  INT         NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    failed_at    TIMESTAMPTZ,
    locked_at    TIMESTAMPTZ,
    last_error   TEXT,

    CONSTRAINT chk_outbox_status CHECK (status IN ('pending','processing','published','failed'))
);

-- Идемпотентность: предотвращает дублирование события
CREATE UNIQUE INDEX idx_outbox_event_id
    ON outbox (event_id);

-- Поиск следующей порции для воркера
CREATE INDEX idx_outbox_pending_available_at
    ON outbox (status, available_at, created_at)
    WHERE status = 'pending';

-- Обнаружение зависших записей (locked, но не завершённых)
CREATE INDEX idx_outbox_processing_locked_at
    ON outbox (locked_at)
    WHERE status = 'processing' AND locked_at IS NOT NULL;
```

#### inbox

```sql
CREATE TABLE inbox (
    id             UUID PRIMARY KEY,
    event_id       UUID        NOT NULL,
    topic          TEXT        NOT NULL,
    consumer_group TEXT        NOT NULL,
    payload        BYTEA       NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'processing',
    received_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at   TIMESTAMPTZ,
    failed_at      TIMESTAMPTZ,
    error_message  TEXT,

    CONSTRAINT chk_inbox_status CHECK (status IN ('processing','processed','failed'))
);

-- Гарантия exactly-once: одно событие на одну consumer group
CREATE UNIQUE INDEX idx_inbox_event_consumer_group
    ON inbox (event_id, consumer_group);

CREATE INDEX idx_inbox_status_received_at
    ON inbox (status, received_at);
```

### spot_db

#### market_store

```sql
CREATE TABLE market_store (
    id         UUID      PRIMARY KEY,
    name       TEXT      NOT NULL,
    enabled    BOOLEAN   NOT NULL,
    deleted_at TIMESTAMPTZ,         -- NULL = активен (soft delete)
    updated_at TIMESTAMPTZ,

    CONSTRAINT chk_market_name CHECK (length(trim(name)) > 0)
);
```

#### outbox (SpotService)

Структура идентична `outbox` в OrderService.  
`event_type`: `"market.state.changed"`

#### market_poller_cursor

```sql
CREATE TABLE market_poller_cursor (
    poller_name   TEXT        PRIMARY KEY,  -- 'market_state_changed_poller'
    last_seen_at  TIMESTAMPTZ NOT NULL,
    last_seen_id  UUID        NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 16. Зависимости между компонентами

### Граф зависимостей OrderService

```
OrderHandler
  └── OrderService
        ├── TransactionManager    ← pgxpool
        ├── Saver                 ← postgres/order_store
        ├── Getter                ← postgres/order_store
        ├── MarketViewer          ← shared/client/grpc/SpotClient
        │     └── CircuitBreaker  ← gobreaker
        ├── MarketBlockStore      ← redis/market_block_store
        ├── RateLimiter (Create)  ← redis/order_rate_limiter
        ├── RateLimiter (Get)     ← redis/order_rate_limiter
        └── EventProducer         ← services/producer/order_producer
              └── outbox_store    ← postgres/outbox_store

CompensationService
  ├── TransactionManager    ← pgxpool
  ├── MarketInboxWriter     ← postgres/inbox_store
  ├── MarketOrderCanceler   ← postgres/order_store
  ├── MarketBlockStore      ← redis/market_block_store
  └── OrderEventProducer    ← services/producer/order_producer

Kafka Consumer (market.state.changed)
  └── CompensationService

Outbox Worker
  └── outbox_store + kafka/producer
```

### Граф зависимостей SpotService

```
SpotInstrumentHandler
  └── MarketViewer (service)
        ├── MarketCache       ← redis/market_cache
        ├── MarketByIDCache   ← redis/market_by_id_cache
        │     └── singleflight (by-id miss path)
        └── MarketStore       ← postgres/market_store

MarketPoller
  ├── MarketReader    ← postgres/market_store
  ├── CursorStore     ← postgres/cursor_store
  └── MarketProducer
        ├── OutboxStore      ← postgres/outbox_store
        └── CacheRefresher   ← redis/market_cache (role-based head-cache)

Outbox Worker
  └── outbox_store + kafka/producer
```

### Внешние зависимости

| Компонент | OrderService | SpotService |
|---|---|---|
| PostgreSQL | `order_db` | `spot_db` |
| Redis | Токены, блокировки, rate limit | Role-based head-cache рынков и by-id cache |
| Kafka | Producer (outbox), Consumer (market.state.changed) | Producer (outbox) |
| SpotService gRPC | ← клиент | — |
| OTel Collector | OTLP gRPC :4317 | OTLP gRPC :4317 |
