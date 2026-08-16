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

## Стек

| Компонент | Технология |
|-----------|------------|
| Язык | Go 1.25+ |
| HTTP | Echo / Chi |
| БД | PostgreSQL |
| Файлы | MinIO (S3) |
| Очереди | RabbitMQ (topic exchange) |
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
avatars-service/
├── cmd/
│   ├── server/          # HTTP API и веб-интерфейс
│   ├── worker/          # Асинхронная обработка
│   └── migrate/         # Применение миграций (goose)
├── internal/
│   ├── api/
│   ├── config/
│   ├── domain/
│   ├── handlers/
│   ├── repository/
│   ├── services/
│   └── worker/
├── pkg/
├── web/                 # Готовый SPA / статика
├── migrations/
├── docker/
├── k8s/
└── tests/
```

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

Поднимаются: **migrate** (однократно), **server**, **worker**, PostgreSQL, MinIO, брокер сообщений.

### Локальная разработка

```bash
# Зависимости инфраструктуры
docker compose up -d postgres minio rabbitmq

# Миграции (goose; отдельно от server/worker)
go run ./cmd/migrate -command up
# или: make migrate

# Сервер и worker
go run ./cmd/server
go run ./cmd/worker
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

## Безопасность (бонус)

- Валидация MIME-типов и magic bytes
- Ограничение размера файлов (10 MB)
- Rate limiting
- CORS
- Проверка `X-User-ID` при операциях изменения

## Roadmap

Проект разделён на три спринта. **Спринт 1 (MVP):** REST API, PostgreSQL, MinIO, асинхронная обработка, Docker Compose, тесты. Последующие спринты — Kubernetes и Observability (Prometheus, Grafana, Loki/ELK/OpenSearch, Jaeger).

## Лицензия

Учебный проект.
