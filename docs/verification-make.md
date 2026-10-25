# Проверка запуска через Makefile

Дата: 25 октября 2026 года (Москва).

Корневые команды выполнялись GNU Make в Linux-контейнере с Docker CLI,
подключённым к Docker Desktop Linux Engine 29.4.2. На стороне вызывающего
контейнера Go, curl и jq не устанавливались: все инструменты проекта
предоставлялись образом tools. Данные существующего стенда сохранены.

| Команда | Фактический результат |
|---|---|
| `make config` | PASS: Compose-конфигурация корректна |
| `make build` | PASS: образы API, ingestion, processor, simulator и tools собраны; Go-программы собирает build/Makefile |
| `make verify` | PASS: check, test-race, smoke и recovery выполнены последовательно |
| `make check` (в verify) | PASS: unit-тесты и vet |
| `make test-race` (в verify) | PASS: unit-тесты с race detector и vet |
| `make smoke` (в verify) | PASS: реальная БД, MQTT, Kafka и REST API; дубликат подавлен, конфликт помещён в карантин |
| `make recovery` (в verify) | PASS: три сообщения опубликованы при остановленном processor и доступны после запуска |
| `make simulate DEVICES=10 ROUNDS=3` | PASS: эмулятор завершил три раунда; sequence 3 доступна через API |
| `make devices latest history ps` | PASS: данные и состояние получены через Makefile |
| `make down` → `make run` → `make wait` → `make latest` → `make ps` | PASS: контейнеры пересозданы с сохранением volumes, все сервисы готовы, непрерывный эмулятор работает; получено измерение sequence 6 новой сессии |

Тест хранения проверил восемь конкурентных попыток записи одного сообщения,
идемпотентность last_seen и порядок latest при запоздавшей доставке.
Тесты используют внутренние DNS-имена Compose, без ручного проброса localhost
внутри проверочных контейнеров.

Это функциональные проверки. Они не являются нагрузочным испытанием и не
доказывают достижение 50 000 сообщений/с или высокую доступность стенда.
