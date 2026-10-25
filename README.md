# Мониторинг телеметрии электротехнических устройств

Бакалаврский дипломный проект на Go. **Этап 1: минимальный сквозной контур.**

```text
Simulator → Mosquitto (MQTT QoS 1) → Ingestion → Kafka → Processor → TimescaleDB
                                                                      ↑
                                                                  REST API
```

Реализованы регистрация устройств, физически согласованная генерация,
валидация, карантин, доставка через Kafka, транзакционная дедупликация,
последние значения и история с курсорами. Есть JSON-логи, health endpoints
и Prometheus-метрики. Пороги, аварии, online/offline, агрегаты, dashboards и
пакетная запись запланированы следующими этапами, см. [архитектуру](docs/architecture/README.md).

## Требования

- Docker Engine с плагином Compose на Ubuntu либо Docker Desktop с Linux containers на Windows.
- Для локальных Go-команд: Go 1.26.3 или совместимый более новый выпуск.
- Свободные порты 1883, 5432, 8080–8083, 19092.
- Для комфортного запуска стенда рекомендуется выделить Docker не менее 4 CPU и 6 ГБ RAM;
  фактическое потребление предстоит измерить.

Команды выполняются из корня репозитория. Первый запуск загружает образы и Go-модули.

## Команды Makefile

Для сокращённых команд установите GNU Make и выполняйте `make` из корня
репозитория. На Windows Makefile также совместим с `mingw32-make`.
На Windows цели `smoke` и `recovery` используют PowerShell 7 (`pwsh`),
на Ubuntu — Bash-скрипты. Для Linux-проверок нужны `curl`, а для `recovery` также `jq`.
Остальные команды вызывают Go и Docker напрямую.

```powershell
make help
make up
make simulate DEVICES=10 INTERVAL=1s ROUNDS=3 SEED=42
make ps
make logs SERVICES=processor TAIL=50
make check
make test-integration
make test-race
make down
```

`make build` собирает четыре Go-программы в `bin/`, `make fmt` форматирует
исходники. `make demo` запускает непрерывный эмулятор. Для `test-integration`,
`smoke` и `recovery` сначала запустите стенд через `make up`.
Не запускайте `recovery` параллельно с остальными сквозными проверками.
`make down` сохраняет данные в volumes. `make restart SERVICES=processor`
перезапускает только выбранный сервис; изменение кода применяется через `make up`.
Переопределять инструменты можно через `GO`, `DOCKER`, `POWERSHELL`, `BASH`, а адрес
тестовой БД — через `TEST_DATABASE_URL`.

## Запуск из Ubuntu

Установите Docker Engine вместе с плагинами Compose и Buildx по
[официальной инструкции Docker для Ubuntu](https://docs.docker.com/engine/install/ubuntu/).
Для следующих команд `docker info` должен выполняться от вашего пользователя;
настройка доступа описана в
[Linux post-install](https://docs.docker.com/engine/install/linux-postinstall/).
Для запуска контейнеров Go на хосте не требуется.

```bash
sudo apt update
sudo apt install -y make curl jq git

cd /path/to/diplom
docker info
docker compose version
make config
make up
make ps

# Дождитесь MQTT-подписки приёмника перед отправкой:
until curl -fsS http://localhost:8081/health/ready >/dev/null; do sleep 1; done
make simulate DEVICES=10 INTERVAL=1s ROUNDS=3
curl -fsS http://localhost:8080/api/v1/devices | jq
curl -fsS http://localhost:8080/api/v1/devices/meter-000001/latest | jq
```

Вместо `/path/to/diplom` укажите путь к клонированному репозиторию. При первом
старте важно дождаться readiness приёмника: иначе сообщения, отправленные
до создания MQTT-подписки, могут не попасть в обработку.

Для непрерывной демонстрации: `make demo`. Для остановки с сохранением данных:
`make down`. Метрики и API доступны по тем же localhost-портам, что на Windows.

Проверки на Ubuntu:

```bash
# Требуются Docker и работающий стенд; Go на хосте не нужен:
make recovery
make test-race

# Дополнительно требуется установленный Go 1.26.3 или совместимый более новый:
make check
make smoke
```

`make smoke` сам ожидает readiness сервисов. Его время ожидания можно изменить:
`TIMEOUT_SECONDS=180 make smoke`. Скрипты также запускаются непосредственно:
`bash scripts/smoke.sh` и `bash scripts/verify-recovery.sh`.

Для Ubuntu в WSL2 включите интеграцию своего дистрибутива с Docker Desktop
и выполняйте Linux-команды в терминале Ubuntu. Путь к этому проекту на диске D:
обычно `/mnt/d/diplom`.

## Запуск из Windows / PowerShell

```powershell
docker compose -f deploy/compose.yaml up -d --build
docker compose -f deploy/compose.yaml ps
Invoke-RestMethod http://localhost:8080/health/ready
```

Генератор автоматически регистрирует устройства через API:

```powershell
docker compose -f deploy/compose.yaml run --rm --build simulator -devices=10 -interval=1s -rounds=10
Invoke-RestMethod http://localhost:8080/api/v1/devices
Invoke-RestMethod http://localhost:8080/api/v1/devices/meter-000001/latest
Invoke-RestMethod 'http://localhost:8080/api/v1/devices/meter-000001/measurements?limit=10'
```

Для непрерывной демонстрации с доступной метрикой генератора:

```powershell
docker compose -f deploy/compose.yaml --profile demo up -d --build
```

MQTT PUBACK генератору не означает завершение записи в БД. После конечного
прогона дождитесь появления последней sequence через API. Данные предыдущих
прогонов остаются; session_id отличает запуски. Seed воспроизводит модель
для фиксированных параметров, временные метки и идентификаторы меняются.
Текущий генератор использует одно MQTT-соединение и последовательную отправку,
поэтому не предназначен для доказательства 50 000 msg/s.

## REST API

Машиночитаемый контракт: [OpenAPI 3.1](api/openapi.yaml).

| Метод | Путь | Назначение |
|---|---|---|
| POST | `/api/v1/devices` | Регистрация: id, name, expected_interval_seconds |
| GET | `/api/v1/devices?limit=100&after=...` | Список, следующий курсор next_after |
| GET | `/api/v1/devices/{id}` | Устройство и last_seen_at |
| GET | `/api/v1/devices/{id}/latest` | Последнее по времени измерение, 404 если данных нет |
| GET | `/api/v1/devices/{id}/measurements` | История по убыванию timestamp/message_id |

Параметры истории: `from`, `to` (RFC3339), `limit` (1–1000). По умолчанию последний
час, максимальный диапазон 7 суток, интервал `[from,to)`. Если `next` не null,
передайте его `before`, `before_message_id` и те же `from`,`to` в следующем запросе.
Для неизменного окна копируйте возвращённые from/to. Список устройств и история
возвращают `items`. last_seen_at пока не преобразуется в online/offline.

Все четыре процесса предоставляют `/health/live`, `/health/ready`, `/metrics`
на портах 8080/8081/8082/8083 соответственно. У генератора HTTP endpoints
доступны после регистрации устройств и подключения MQTT, только пока он работает.
HTTP readiness процессора проверяет БД и Kafka, но не является измерением lag.

## Телеметрия

MQTT topic: `devices/{device_id}/telemetry`. Протокол первого этапа — MQTT 3.1.1.

```json
{
  "schema_version": 1,
  "message_id": "unique-message-id",
  "device_id": "meter-000001",
  "session_id": "unique-session-id",
  "sequence": 1,
  "timestamp": "2026-10-25T12:00:00Z",
  "voltage_v": 230,
  "current_a": 5,
  "active_power_w": 1092.5,
  "power_factor": 0.95,
  "temperature_c": 35
}
```

При ручной публикации замените timestamp текущим временем UTC. Принимаются
сообщения возрастом до 24 часов и с опережением до 30 секунд. Максимум 4096 байт.
Идентификаторы: 1–64 символа, латинские буквы, цифры, `_`, `-`, первый символ
буква или цифра. Неизвестные поля отклоняются. `P=U×I×cosφ`, погрешность
проверки max(1 Вт, 2%). Диапазоны: U (0,1000], I [0,10000], P ≥ 0,
cosφ (0,1], T [-100,300]. Это границы допустимых данных, не аварийные пороги.

Устройство сначала регистрируется. Повтор сообщения сохраняет все его поля,
включая message_id и timestamp. Неизвестные устройства, некорректные данные и
конфликты содержимого направляются в Kafka topic `telemetry.invalid.v1`.
Карантин хранит ограниченный payload и причину; он не должен содержать секреты.

## Проверки

```powershell
go test ./...
go vet ./...
./scripts/smoke.ps1
./scripts/verify-recovery.ps1
docker build -f deploy/Dockerfile.test -t diplom-tests .
```

Smoke требует запущенного Compose-стенда. Он проверяет конкурентную
дедупликацию в реальной БД, конфликт содержимого, порядок последних значений,
курсор истории и полный путь MQTT → Kafka → БД → API.
Сквозной тест оставляет одно устройство с префиксом e2e и одно измерение для
проверки вручную; тест хранения удаляет собственные данные.

verify-recovery временно останавливает processor, публикует три измерения
через MQTT и проверяет их появление после запуска. Не выполняйте этот сценарий
параллельно с другими тестами. Он оставляет устройство с префиксом recovery.
Dockerfile.test запускает unit-тесты с race detector и go vet в Linux.

`go test -race ./...` выполняется в среде с доступным C-компилятором.
На Windows без gcc проверку можно выполнить в Linux Go-контейнере.

## Эксплуатация стенда

```powershell
docker compose -f deploy/compose.yaml logs --tail=100 ingestion processor api
docker compose -f deploy/compose.yaml stop processor
# Отправьте конечный прогон генератора; затем восстановите обработку:
docker compose -f deploy/compose.yaml start processor
docker compose -f deploy/compose.yaml down
```

`down` сохраняет named volumes. Миграция 001 применяется автоматически только
к пустому PostgreSQL volume. Существующие БД не мигрируются перезапуском:
новые миграции будут добавляться отдельно. Не удаляйте volumes с нужными данными.

Адреса приложений/БД/MQTT опубликованы только на loopback. Внутри локального
Compose используются учебные учётные данные и MQTT без аутентификации;
этот профиль предназначен для разработки на доверенной машине.

Настройки Go-процессов через env: DATABASE_URL, MQTT_URL, KAFKA_BROKERS,
KAFKA_TOPIC, KAFKA_INVALID_TOPIC, KAFKA_GROUP (processor), MQTT_CLIENT_ID
(ingestion), API_URL (simulator), HTTP_ADDR. При локальном запуске Go
задайте `KAFKA_BROKERS=localhost:19092`; внутри Compose адрес `kafka:9092`.
На этом этапе запускается ровно один ingestion с постоянным client ID.

Гарантии, ограничения отказоустойчивости и следующие этапы описаны в
[архитектуре](docs/architecture/README.md) и [ADR 001](docs/adr/001-initial-boundaries.md).
Экспериментальных результатов нагрузки пока нет.

Результаты выполненных функциональных проверок: [отчёт этапа 1](docs/verification-stage-1.md).
