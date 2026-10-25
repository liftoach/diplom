.DEFAULT_GOAL := help

GO ?= go
DOCKER ?= docker
POWERSHELL ?= pwsh
COMPOSE = $(DOCKER) compose -f deploy/compose.yaml

DEVICES ?= 10
INTERVAL ?= 1s
ROUNDS ?= 10
SEED ?= 42
SERVICES ?=
TAIL ?= 100
export TEST_DATABASE_URL ?= postgres://telemetry:telemetry@localhost:5432/telemetry?sslmode=disable

.PHONY: help build up down stop restart ps logs config demo simulate \
        fmt test vet check test-race test-integration smoke recovery

help:
	@echo make up                - Build and start the local stack
	@echo make down              - Remove containers and keep data volumes
	@echo make build             - Build all four Go binaries into bin/
	@echo make stop              - Stop services without removing containers
	@echo make restart           - Restart services, optionally SERVICES=processor
	@echo make ps                - Show container status
	@echo make logs              - Follow logs, optionally SERVICES=processor TAIL=50
	@echo make config            - Validate Docker Compose configuration
	@echo make demo              - Start the stack with a continuous simulator
	@echo make simulate          - Run a finite simulator, DEVICES=10 INTERVAL=1s ROUNDS=10 SEED=42
	@echo make fmt               - Format Go source files
	@echo make test              - Run unit tests
	@echo make vet               - Run go vet
	@echo make check             - Run unit tests and go vet
	@echo make test-race         - Run unit tests with race detector and vet in Docker
	@echo make test-integration  - Run database and pipeline tests against the running stack
	@echo make smoke             - Wait for readiness and run integration tests, requires PowerShell 7
	@echo make recovery          - Verify processor recovery, requires PowerShell 7 and the running stack

build:
	$(GO) build -o bin/ ./cmd/...

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) --profile demo down

stop:
	$(COMPOSE) --profile demo stop $(SERVICES)

restart:
	$(COMPOSE) --profile demo restart $(SERVICES)

ps:
	$(COMPOSE) --profile demo ps

logs:
	$(COMPOSE) --profile demo logs --follow --tail=$(TAIL) $(SERVICES)

config:
	$(COMPOSE) --profile demo config --quiet

demo:
	$(COMPOSE) --profile demo up -d --build

simulate:
	$(COMPOSE) run --rm --build simulator -devices=$(DEVICES) -interval=$(INTERVAL) -rounds=$(ROUNDS) -seed=$(SEED)

fmt:
	gofmt -w cmd internal tests

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: test vet

test-race:
	$(DOCKER) build -f deploy/Dockerfile.test -t diplom-tests .

test-integration:
	$(GO) test -tags=integration -count=1 -v ./internal/storage ./tests/e2e

smoke:
	$(POWERSHELL) -NoProfile -File scripts/smoke.ps1

recovery:
	$(POWERSHELL) -NoProfile -File scripts/verify-recovery.ps1
