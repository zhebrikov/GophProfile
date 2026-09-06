# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

# Runtime stage
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata \
    && adduser -D -u 65532 -g app app \
    && mkdir -p /app \
    && chown -R app:app /app
WORKDIR /app

COPY --from=builder --chown=app:app /out/server /app/server
COPY --from=builder --chown=app:app /out/worker /app/worker
COPY --from=builder --chown=app:app /out/migrate /app/migrate
COPY --from=builder --chown=app:app /app/web /app/web
COPY --from=builder --chown=app:app /app/migrations /app/migrations

ENV WEB_DIR=/app/web
USER 65532:65532
EXPOSE 8080
