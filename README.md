# GophProfile

Микросервис для управления аватарками пользователей. Пользователь загружает фотографию один раз — сторонние платформы (блоги, форумы, сервисы комментариев и др.) могут запросить аватар по идентификатору пользователя. Если аватар найден, сервис отдаёт изображение; иначе — стандартную заглушку.

## Возможности

- Загрузка аватарок через REST API и веб-интерфейс
- Хранение оригиналов и миниатюр в S3-совместимом хранилище (MinIO)
- Метаданные в PostgreSQL
- Асинхронная обработка изображений через RabbitMQ
- Создание миниатюр 100×100 и 300×300
- Мягкое удаление с асинхронной очисткой файлов в S3
- Health / live / ready probes, circuit breaker для Postgres / S3 / RabbitMQ
- Rate limiting, graceful shutdown, non-root контейнеры
- Docker Compose (local observability) и Helm Chart (Kubernetes)
- Observability: OpenTelemetry → Jaeger, Prometheus + Grafana, OpenSearch, Alertmanager; в K8s — ServiceMonitor

## Стек

| Компонент | Технология |
|-----------|------------|
| Язык | Go 1.25+ |
| HTTP | Echo |
| БД | PostgreSQL |
| Файлы | MinIO (S3) |
| Очереди | RabbitMQ (topic exchange) |
| Метрики | Prometheus (+ ServiceMonitor в K8s) |
| Логи | slog (JSON) → Fluent Bit → OpenSearch |
| Трейсы | OpenTelemetry → Jaeger |
| Дашборды / алерты | Grafana, OpenSearch Dashboards, Alertmanager |
| Оркестрация | Docker Compose, Kubernetes, Helm |

## Архитектура

### Приложение

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
                    │  RabbitMQ    │            │
                    └──────┬───────┘            │
                           │                    │
                           ▼                    │
                    ┌──────────────┐            │
                    │    Worker    │────────────┘
                    │ (thumbnails, │
                    │   cleanup)   │
                    └──────────────┘
```

### Kubernetes

```
                    ┌─────────────┐
                    │   Ingress   │
                    └──────┬──────┘
                           │
              ┌────────────▼────────────┐
              │  Service (server)       │◀── HPA (CPU/RAM)
              │  Deployment ×N          │
              │  /live /ready /metrics  │
              └────────────┬────────────┘
        ┌──────────────────┼──────────────────┐
        ▼                  ▼                  ▼
   PostgreSQL           MinIO            RabbitMQ
        ▲                  ▲                  ▲
        └────────── Worker Deployment ────────┘
                         ▲
                    ServiceMonitor
                         │
                    Prometheus Operator
```

Миграции БД: Helm Job (`post-install` / `pre-upgrade`) и initContainer у server/worker (идемпотентный `goose up`, без гонки на первом install). NetworkPolicy ограничивает трафик между компонентами. Секреты — в Secret; non-secret конфиг — в ConfigMap.

### Компоненты

- **HTTP-сервер** — REST API и веб-интерфейс
- **Worker** — фоновая обработка изображений и удаление файлов
- **PostgreSQL** — метаданные аватарок
- **MinIO** — хранение оригиналов и миниатюр
- **RabbitMQ** — очередь событий обработки

## Структура проекта

```
GophProfile/
├── cmd/
│   ├── server/          # HTTP API и веб-интерфейс
│   ├── worker/          # Асинхронная обработка
│   └── migrate/         # Применение миграций (goose)
├── internal/
│   └── circuitbreaker/  # Circuit breaker (gobreaker)
├── pkg/                 # broker, storage, imageutil
├── helm/gophprofile/    # Helm Chart (K8s)
├── k8s/                 # Ссылка на Helm
├── docs/openapi.yaml    # OpenAPI 3 спецификация
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

**Метрики (business):** `gophprofile_avatars_uploaded_total`, `gophprofile_avatars_processed_total`, `gophprofile_avatar_processing_duration_seconds`, `gophprofile_avatars_deleted_total`, HTTP RED, `gophprofile_circuit_breaker_state`, `gophprofile_circuit_breaker_trips_total`.

**Логи:** структурированный JSON (`slog`) с `request_id`, `trace_id`, `span_id`; Fluent Bit забирает stdout контейнеров `server`/`worker` в OpenSearch (`gophprofile-logs-*`).

**Алерты:** high 5xx rate, high p95 latency, processing failures, health component down. Правила: [`monitoring/prometheus/alerts.yml`](monitoring/prometheus/alerts.yml) (Compose) и Helm `PrometheusRule` (Kubernetes).

### Kubernetes monitoring

В кластере с **kube-prometheus-stack**:

1. Chart создаёт `ServiceMonitor` для server (`:8080/metrics`) и worker (`:9091/metrics`).
2. Chart создаёт `PrometheusRule` с теми же алертами, что в Compose.
3. NetworkPolicy пускает scrape server из namespace `monitoring` (`networkPolicy.metrics`).
4. **Состояние кластера** (ноды, поды, HPA, deployments) — встроенные дашборды kube-prometheus-stack / Kubernetes / Compute Resources.
5. **Состояние приложения** — импортируйте [`monitoring/grafana/dashboards/gophprofile-overview.json`](monitoring/grafana/dashboards/gophprofile-overview.json).

Включение:
```bash
helm upgrade --install gophprofile ./helm/gophprofile \
  --set serviceMonitor.enabled=true \
  --set prometheusRule.enabled=true \
  --set serviceMonitor.labels.release=kube-prometheus-stack \
  --set prometheusRule.labels.release=kube-prometheus-stack
```
(или используйте `values-prod.yaml`, где это уже включено).

### Проверка алертов (тестовые сценарии)

1. **HealthComponentDown** — остановите зависимость и дергайте health:
   ```bash
   docker compose stop postgres
   curl -s http://localhost:8080/health
   docker compose start postgres
   ```
2. **HighHTTPErrorRate** — доля 5xx > 5% в течение 2 минут.
3. **AvatarProcessingFailures** — worker пишет `status=failed`; через ~2 мин сработает алерт.

После срабатывания: http://localhost:9093/#/alerts и http://localhost:9094.

## API

OpenAPI 3 спецификация: [`docs/openapi.yaml`](docs/openapi.yaml).

### Загрузка аватарки

```http
POST /api/v1/avatars
Content-Type: multipart/form-data
X-User-ID: <user_id>
```

| Параметр | Описание |
|----------|----------|
| `file` | Бинарный файл (JPEG, PNG, WebP), до 10 MB |

**201 Created** · **400** неверный формат · **413** слишком большой

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

### Метаданные и список

```http
GET /api/v1/avatars/{avatar_id}/metadata
GET /api/v1/users/{user_id}/avatars
```

### Удаление

```http
DELETE /api/v1/avatars/{avatar_id}
DELETE /api/v1/users/{user_id}/avatar
X-User-ID: <user_id>
```

Мягкое удаление в БД, асинхронная очистка в S3. **204** / **403**.

### Health / probes

```http
GET /live    # liveness — процесс жив
GET /ready   # readiness — Postgres, S3, broker
GET /health  # полный статус зависимостей
```

### Веб-интерфейс

| Метод | Путь | Описание |
|-------|------|----------|
| `GET` | `/web/upload` | Форма загрузки |
| `POST` | `/web/upload` | Обработка загрузки |
| `GET` | `/web/gallery/{user_id}` | Галерея аватарок |

## Модель данных

Таблица `avatars` (UUID, user_id, file metadata, s3_key, thumbnails JSONB, soft-delete) и `processed_messages` для идемпотентности. См. [`migrations/`](migrations/).

## Асинхронная обработка

После загрузки сервер публикует событие в брокер. Worker загружает оригинал из S3, создаёт миниатюры 100×100 и 300×300, сохраняет в S3 и обновляет статус в БД. Обработка идемпотентна (retry + backoff). Вызовы к Postgres / S3 / RabbitMQ защищены circuit breaker.

## Быстрый старт (Docker Compose)

### Требования

- Go 1.25+
- Docker и Docker Compose

### Запуск

```bash
docker compose up -d --build
```

Поднимаются: migrate, server, worker, PostgreSQL, MinIO, RabbitMQ, Jaeger, Prometheus, Alertmanager, OpenSearch, Fluent Bit, Grafana.

### Локальная разработка

```bash
docker compose up -d postgres minio rabbitmq jaeger
go run ./cmd/migrate -command up
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318 go run ./cmd/server
OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318 go run ./cmd/worker
```

### Тесты

```bash
make test
make cover
```

## Kubernetes / Helm

Chart: [`helm/gophprofile/`](helm/gophprofile/). Включает server, worker, migrate hook, Ingress, HPA, ServiceMonitor, NetworkPolicy, RBAC/SecurityContext и (по умолчанию) Postgres + MinIO + RabbitMQ для локального кластера.

### Требования

- Kubernetes (например Rancher Desktop)
- Helm 3
- Ingress NGINX
- (опционально) kube-prometheus-stack для ServiceMonitor / PrometheusRule

### Локальный деплой (Rancher Desktop)

```bash
docker build -t gophprofile:local .

kubectl create namespace gophprofile

helm upgrade --install gophprofile ./helm/gophprofile \
  --namespace gophprofile \
  --wait

# Если установлен kube-prometheus-stack:
#   --set serviceMonitor.enabled=true

# /etc/hosts: 127.0.0.1 gophprofile.local
curl -s http://gophprofile.local/live
curl -s http://gophprofile.local/ready
curl -s http://gophprofile.local/health
```

Port-forward без Ingress:

```bash
kubectl -n gophprofile port-forward svc/gophprofile-server 8080:8080
```

### Production values

```bash
kubectl -n gophprofile create secret generic gophprofile-secrets \
  --from-literal=DATABASE_URL='postgres://...' \
  --from-literal=S3_ACCESS_KEY='...' \
  --from-literal=S3_SECRET_KEY='...' \
  --from-literal=BROKER_URL='amqp://...'

helm upgrade --install gophprofile ./helm/gophprofile \
  --namespace gophprofile \
  -f ./helm/gophprofile/values-prod.yaml \
  --set image.repository=your.registry/gophprofile \
  --set image.tag=1.0.0
```

В `values-prod.yaml`: `infra.enabled=false`, внешние зависимости, более жёсткие resources/HPA.

### Что входит в chart

| Ресурс | Назначение |
|--------|------------|
| Deployment server/worker | Приложение, probes, preStop, resource limits, non-root |
| Service + Ingress | Маршрутизация и load balancing |
| ConfigMap / Secret | Конфиг и секреты |
| HPA | Автоскейл по CPU 70% / memory 80% |
| Job migrate | Helm hook миграций БД (`post-install` / `pre-upgrade`) |
| initContainer migrate | Гарантирует схему до старта server/worker |
| ServiceMonitor | Скрейп `/metrics` Prometheus Operator |
| PrometheusRule | Алерты приложения в кластере |
| NetworkPolicy | Ограничение ingress/egress (+ scrape из `monitoring`) |
| ServiceAccount + Role | Минимальные права |
| Postgres / MinIO / RabbitMQ | Локальная infra (`infra.enabled`); MinIO также через Ingress `/s3` |

Worker probes: `GET :9091/live` (liveness) и `GET :9091/ready` (Postgres / S3 / RabbitMQ).

## Конфигурация

| Переменная | Описание |
|------------|----------|
| `HTTP_ADDR` | Адрес HTTP-сервера |
| `DATABASE_URL` | Строка подключения к PostgreSQL |
| `S3_ENDPOINT` | Endpoint MinIO / S3 |
| `S3_ACCESS_KEY` / `S3_SECRET_KEY` | Credentials S3 |
| `S3_BUCKET` | Имя бакета |
| `BROKER_URL` | URL RabbitMQ |
| `OTEL_SERVICE_NAME` | Имя сервиса в трейсах |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP HTTP endpoint (`host:port`) |
| `METRICS_ADDR` | Адрес `/metrics` у worker |
| `RATE_LIMIT` | Лимит запросов в секунду на IP |

## Production-готовность

- Graceful shutdown (SIGINT/SIGTERM + `preStop` в K8s)
- Circuit breaker для Postgres, S3, RabbitMQ publish
- Rate limiting (token bucket per IP)
- Liveness `/live` и readiness `/ready` (server и worker)
- Resource requests/limits + HPA
- Secrets для credentials, non-root UID 65532
- NetworkPolicy между компонентами
- Ingress TLS (через `ingress.tls` + cert-manager в `values-prod.yaml`)
- Proper error handling и идемпотентная обработка очереди

## Безопасность

- Валидация MIME-типов и magic bytes
- Ограничение размера файлов (10 MB)
- Rate limiting, CORS, проверка `X-User-ID`
- Non-root контейнеры, `allowPrivilegeEscalation: false`, drop ALL capabilities
- RBAC: ServiceAccount без лишних прав на API-сервер

## Roadmap

**Спринт 1 (MVP):** REST API, PostgreSQL, MinIO, асинхронная обработка, Docker Compose, тесты.  
**Observability:** Prometheus, Grafana, OpenSearch, Jaeger, Alertmanager — реализовано.  
**Kubernetes:** Helm Chart, HPA, ServiceMonitor, PrometheusRule, NetworkPolicy, circuit breaker — реализовано.

## Лицензия

Учебный проект.
