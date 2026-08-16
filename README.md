# GophProfile

Микросервис для управления аватарками пользователей. Пользователь загружает фотографию один раз — сторонние платформы (блоги, форумы, сервисы комментариев и др.) могут запросить аватар по идентификатору пользователя. Если аватар найден, сервис отдаёт изображение; иначе — стандартную заглушку.

## Возможности

- Загрузка аватарок через REST API и веб-интерфейс
- Хранение оригиналов и миниатюр в S3-совместимом хранилище (MinIO)
- Метаданные в PostgreSQL
- Асинхронная обработка изображений через брокер сообщений (RabbitMQ / Kafka)
- Создание миниатюр 100×100 и 300×300
- Мягкое удаление с асинхронной очисткой файлов в S3
- Healthcheck с проверкой БД, S3 и брокера
- Docker и Docker Compose для локального запуска
- Observability: OpenTelemetry → Jaeger, Prometheus + Grafana, OpenSearch + Dashboards, Alertmanager

## Стек

| Компонент | Технология |
|-----------|------------|
| Язык | Go 1.25+ |
| HTTP | Echo |
| БД | PostgreSQL |
| Файлы | MinIO (S3) |
| Очереди | RabbitMQ (topic exchange) |
| Метрики | Prometheus |
| Логи | slog (JSON) → Fluent Bit → OpenSearch |
| Трейсы | OpenTelemetry → Jaeger |
| Дашборды / алерты | Grafana, OpenSearch Dashboards, Alertmanager |
| Контейнеры | Docker, Docker Compose |

## Архитектура

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│  Client /   │────▶│ HTTP Server  │────▶│ PostgreSQL  │
│  Frontend   │     │  (REST+Web)  │     └─────────────┘
└─────────────┘     └──────┬───────┘            │
                           │                    │
                           ├───────────────────▶│ MinIO (S3)
                           │                    │
                           ▼                    │
                    ┌──────────────┐            │
                    │ Message Bus  │            │
                    │ RabbitMQ/    │            │
                    │ Kafka        │            │
                    └──────┬───────┘            │
                           │                    │
                           ▼                    │
                    ┌──────────────┐            │
                    │    Worker    │────────────┘
                    │ (thumbnails, │
                    │   cleanup)   │
                    └──────────────┘
```

### Компоненты

- **HTTP-сервер** — REST API и веб-интерфейс
- **Worker** — фоновая обработка изображений и удаление файлов
- **PostgreSQL** — метаданные аватарок
- **MinIO** — хранение оригиналов и миниатюр
- **RabbitMQ / Kafka** — очередь событий обработки

## Структура проекта

```
GophProfile/
├── cmd/
│   ├── server/          # HTTP API и веб-интерфейс
│   ├── worker/          # Асинхронная обработка
│   └── migrate/         # Применение миграций (goose)
├── internal/
│   ├── config/
│   ├── domain/
│   ├── handlers/
│   ├── middleware/
│   ├── observability/   # логи, метрики, трейсинг
│   ├── repository/
│   ├── services/
│   └── worker/
├── pkg/
├── monitoring/          # Prometheus, Grafana, OpenSearch, Fluent Bit, Alertmanager
├── web/
├── migrations/
└── docker-compose.yml
```

## Observability

После `docker compose up -d --build` доступны:

| Сервис | URL | Назначение |
|--------|-----|------------|
| Grafana | http://localhost:3000 (admin/admin) | Дашборды, Explore (логи/метрики/трейсы) |
| Prometheus | http://localhost:9090 | Метрики и правила алертов |
| Alertmanager | http://localhost:9093 | Алерты |
| Alert webhook | http://localhost:9094 | Приём webhook-нотификаций алертов |
| Jaeger | http://localhost:16686 | Distributed tracing |
| OpenSearch | http://localhost:9200 | Индекс логов |
| OpenSearch Dashboards | http://localhost:5601 | Поиск и фильтрация логов |
| App metrics | http://localhost:8080/metrics | Server scrape |
| Worker metrics | http://localhost:9091/metrics | Worker scrape |

**Трейсинг:** W3C Trace Context; спаны HTTP → service → repository/S3/RabbitMQ → worker. Корреляция через `trace_id` в JSON-логах и в Jaeger.

**Метрики (business):** `gophprofile_avatars_uploaded_total`, `gophprofile_avatars_processed_total`, `gophprofile_avatar_processing_duration_seconds`, `gophprofile_avatars_deleted_total`, HTTP RED.

**Логи:** структурированный JSON (`slog`) с `request_id`, `trace_id`, `span_id`; Fluent Bit забирает stdout контейнеров `server`/`worker` в OpenSearch (`gophprofile-logs-*`). Поиск: OpenSearch Dashboards (Discover) или Grafana Explore → OpenSearch. При первом заходе в Dashboards создайте index pattern `gophprofile-logs*`, time field `@timestamp`. Примеры фильтров: `service:server`, `trace_id:<id>`, `level:ERROR`.

**Алерты:** high 5xx rate, high p95 latency, processing failures, health component down. Нотификации уходят в Alertmanager UI и на webhook `:9094`.

### Проверка алертов (тестовые сценарии)

1. **HealthComponentDown** — остановите зависимость и дергайте health:
   ```bash
   docker compose stop postgres
   curl -s http://localhost:8080/health
   # через ~1–2 мин: Prometheus → Alerts / Alertmanager → HealthComponentDown
   docker compose start postgres
   ```
2. **HighHTTPErrorRate** — много запросов к несуществующему ресурсу с ошибками сервера (или временно сломайте S3) до доли 5xx > 5% в течение 2 минут.
3. **AvatarProcessingFailures** — опубликуйте событие с невалидным `s3_key`, чтобы worker писал `status=failed`; через ~2 мин сработает алерт.

После срабатывания проверьте:
- http://localhost:9093/#/alerts
- http://localhost:9094 (тело webhook от Alertmanager)
## API

### Загрузка аватарки

```http
POST /api/v1/avatars
Content-Type: multipart/form-data
X-User-ID: <user_id>
```

| Параметр | Описание |
|----------|----------|
| `file` | Бинарный файл (JPEG, PNG, WebP), до 10 MB |

**201 Created**

```json
{
  "id": "uuid",
  "user_id": "string",
  "url": "string",
  "status": "processing",
  "created_at": "2024-01-01T00:00:00Z"
}
```

**400** — неверный формат · **413** — файл слишком большой

### Получение аватарки

```http
GET /api/v1/avatars/{avatar_id}
GET /api/v1/avatars/{avatar_id}?size=300x300&format=webp
GET /api/v1/users/{user_id}/avatar
```

| Query | Значения |
|-------|----------|
| `size` | `100x100`, `300x300`, `original` |
| `format` | `jpeg`, `png`, `webp` |

Ответ: бинарные данные изображения (`Content-Type`, `Cache-Control`, `ETag`).

### Метаданные

```http
GET /api/v1/avatars/{avatar_id}/metadata
```

```json
{
  "id": "uuid",
  "user_id": "string",
  "file_name": "avatar.jpg",
  "mime_type": "image/jpeg",
  "size": 1024000,
  "dimensions": { "width": 1920, "height": 1080 },
  "thumbnails": [
    { "size": "100x100", "url": "..." },
    { "size": "300x300", "url": "..." }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

### Список аватарок пользователя

```http
GET /api/v1/users/{user_id}/avatars
```

### Удаление

```http
DELETE /api/v1/avatars/{avatar_id}
DELETE /api/v1/users/{user_id}/avatar
X-User-ID: <user_id>
```

Мягкое удаление в БД, асинхронная очистка в S3. **204** при успехе, **403** если аватар чужой.

### Healthcheck

```http
GET /health
```

JSON со статусами PostgreSQL, S3 и брокера сообщений.

### Веб-интерфейс

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/web/upload` | Форма загрузки |
| `POST` | `/web/upload` | Обработка загрузки |
| `GET` | `/web/gallery/{user_id}` | Галерея аватарок |

## Модель данных

```sql
CREATE TABLE avatars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    s3_key VARCHAR(500) NOT NULL,
    thumbnail_s3_keys JSONB,
    upload_status VARCHAR(50) DEFAULT 'uploading',
    processing_status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

CREATE INDEX idx_avatars_user_id ON avatars(user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_avatars_status ON avatars(upload_status, processing_status);
```

## Асинхронная обработка

После загрузки сервер публикует событие в брокер. Worker:

1. Загружает оригинал из S3
2. Создаёт миниатюры 100×100 и 300×300
3. Сохраняет их в S3
4. Обновляет статус в БД

### События

```go
type AvatarUploadEvent struct {
    AvatarID string `json:"avatar_id"`
    UserID   string `json:"user_id"`
    S3Key    string `json:"s3_key"`
}

type AvatarProcessEvent struct {
    AvatarID   string         `json:"avatar_id"`
    Operations []ProcessingOp `json:"operations"`
}

type AvatarDeleteEvent struct {
    AvatarID string   `json:"avatar_id"`
    S3Keys   []string `json:"s3_keys"`
}
```

Обработка идемпотентна: уникальные ID сообщений, проверка статуса перед работой, retry с экспоненциальным backoff.

## Быстрый старт

### Требования

- Go 1.25+
- Docker и Docker Compose

### Запуск окружения

```bash
docker compose up -d --build
```

Поднимаются: **migrate**, **server**, **worker**, PostgreSQL, MinIO, RabbitMQ, **Jaeger**, **Prometheus**, **Alertmanager**, **OpenSearch**, **Fluent Bit**, **Grafana**.

### Локальная разработка

```bash
# Зависимости инфраструктуры
docker compose up -d postgres minio rabbitmq jaeger

# Миграции (goose; отдельно от server/worker)
go run ./cmd/migrate -command up
# или: make migrate

# Сервер и worker (с экспортом трейсов в локальный Jaeger)
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318 go run ./cmd/server
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318 go run ./cmd/worker
```
Откат последней миграции: `go run ./cmd/migrate -command down`. Статус: `go run ./cmd/migrate -command status`.

Если PostgreSQL уже поднимался со старой схемой без goose, пересоздайте том: `docker compose down -v`, затем снова `docker compose up -d --build`.

### Тесты

```bash
make test
make cover   # цель: >50% на internal/ и pkg/
```

Цель покрытия unit-тестами — **>50%**. Рекомендуемые инструменты: `testify`, `golangci-lint`.

## Конфигурация

Основные переменные окружения (пример):

| Переменная | Описание |
|------------|----------|
| `HTTP_ADDR` | Адрес HTTP-сервера |
| `DATABASE_URL` | Строка подключения к PostgreSQL |
| `S3_ENDPOINT` | Endpoint MinIO / S3 |
| `S3_ACCESS_KEY` | Access key |
| `S3_SECRET_KEY` | Secret key |
| `S3_BUCKET` | Имя бакета |
| `BROKER_URL` | URL RabbitMQ / Kafka |
| `OTEL_SERVICE_NAME` | Имя сервиса в трейсах |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP endpoint (`host:port`, без схемы) |
| `METRICS_ADDR` | Адрес `/metrics` у worker (по умолчанию `:9091`) |

## Безопасность (бонус)

- Валидация MIME-типов и magic bytes
- Ограничение размера файлов (10 MB)
- Rate limiting
- CORS
- Проверка `X-User-ID` при операциях изменения

## Roadmap

**Спринт 1 (MVP):** REST API, PostgreSQL, MinIO, асинхронная обработка, Docker Compose, тесты.  
**Observability:** Prometheus, Grafana, OpenSearch, Jaeger, Alertmanager — реализовано.
Последующие спринты — Kubernetes.

## Лицензия

Учебный проект.
