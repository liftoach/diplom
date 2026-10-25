# Проверка первого этапа

Дата: 25 октября 2026 года (Москва).

Среда: Windows, Go 1.26.3 windows/amd64, Docker Desktop Linux Engine 29.4.2.
Версии приложений и зависимостей закреплены в Compose и go.mod/go.sum.

## Выполненные проверки

| Проверка | Фактический результат |
|---|---|
| `go test ./...` | PASS |
| `go vet ./...` | PASS |
| `docker compose -f deploy/compose.yaml config --quiet` | PASS |
| Сборка и запуск Compose | PASS, Kafka и TimescaleDB healthy |
| `scripts/smoke.ps1` | PASS: реальная БД и полный путь через MQTT/Kafka/API |
| `scripts/verify-recovery.ps1` | PASS: три сообщения, опубликованные при остановленном processor, сохранены после его запуска |
| `docker build -f deploy/Dockerfile.test -t diplom-tests .` | PASS: `go test -p 2 -race ./...` и `go vet ./...` в Linux |
| Интеграционный тест хранения с race detector | PASS: восемь конкурентных попыток дают ровно одну вставку |
| Контейнерный simulator, 10 устройств × 3 раунда | Через REST API сверены все 30 измерений одного session_id |
| `/health/ready` на 8080, 8081, 8082 после испытаний | HTTP 200 |
| Метрики processor после восстановления и генератора | 33 вставки: 3 recovery + 30 simulator |
| `git diff --check` | PASS |

Сквозной тест дополнительно проверяет подавление повторного сообщения и
публикацию конфликта содержимого в `telemetry.invalid.v1`.
Тест БД проверяет, что повтор не продлевает last_seen, а запоздавшее
измерение не заменяет последнее значение и корректно доступно в истории.

Команда интеграционной проверки гонок после сборки diplom-tests:

```powershell
docker run --rm --network diplom_default -e TEST_DATABASE_URL=postgres://telemetry:telemetry@database:5432/telemetry?sslmode=disable diplom-tests go test -race -tags=integration -count=1 -v ./internal/storage
```

Для демонстрации использован session_id `4865f78e1255bda078594e47504c91bf`.
В тестах e2e и recovery создаются уникальные устройства; они остаются в БД.

## Ограничения выводов

Это функциональная проверка первого вертикального среза. Она не измеряет
устойчивую пропускную способность, p95/p99 или предельное число соединений.
Испытания на 100 / 1 000 / 10 000 устройств и 50 000 сообщений/с ещё не выполнены.
Потеря диска/хоста, длительный отказ брокеров и переполнение очередей не испытаны.
Race detector проверил выполненные тестовые пути; отдельное испытание всех
работающих сервисов под race detector ещё не проводилось.
