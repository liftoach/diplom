.DEFAULT_GOAL := help
.DELETE_ON_ERROR:

OS ?=
DOCKER ?= docker
COMPOSE = $(DOCKER) compose -f deploy/compose.yaml
TOOL = $(COMPOSE) --profile tools run --rm --no-deps -e DEVICE_ID=$(DEVICE_ID) tools make -f build/Makefile

DEVICES ?= 10
INTERVAL ?= 1s
ROUNDS ?= 10
SEED ?= 42
DEVICE_ID ?= meter-000001
SERVICES ?=
TAIL ?= 100
export DEVICES INTERVAL SEED

ifeq ($(OS),Windows_NT)
LOCAL_USER ?= 0:0
else
LOCAL_USER ?= $(shell id -u):$(shell id -g)
endif

.PHONY: help tools build up run demo simulate wait down stop restart ps logs config \
        fmt test vet check test-race test-integration smoke recovery verify devices latest history

# Recovery stops processor; verification targets must run sequentially.
.NOTPARALLEL:

help:
	@echo make run               - Build, wait for readiness and run the continuous demo
	@echo make up                - Build and start the stack, wait for readiness
	@echo make build             - Build application and development Docker images
	@echo make simulate          - Start the stack and publish DEVICES=10 INTERVAL=1s ROUNDS=10 SEED=42
	@echo make devices           - Show registered devices
	@echo make latest            - Show latest measurement, DEVICE_ID=meter-000001
	@echo make history           - Show device history, DEVICE_ID=meter-000001
	@echo make wait              - Wait for API, ingestion and processor readiness
	@echo make ps                - Show container status
	@echo make logs              - Follow logs, optionally SERVICES=processor TAIL=50
	@echo make restart           - Restart services and wait for readiness
	@echo make stop              - Stop services, optionally SERVICES=processor
	@echo make down              - Remove containers and keep data volumes
	@echo make config            - Validate Docker Compose configuration
	@echo make fmt               - Format Go source files in a container
	@echo make test              - Run unit tests in a container
	@echo make vet               - Run go vet in a container
	@echo make check             - Run unit tests and vet in a container
	@echo make test-race         - Run race detector and vet in a container
	@echo make test-integration  - Start the stack and run database and pipeline tests
	@echo make smoke             - Start the stack and verify the complete message pipeline
	@echo make recovery          - Start the stack and verify processor recovery
	@echo make verify            - Run check, race, smoke and recovery sequentially

tools:
	$(COMPOSE) --profile tools build tools

build:
	$(COMPOSE) --profile demo --profile tools build api ingestion processor simulator tools

up: tools
	$(COMPOSE) up -d --build api ingestion processor
	$(TOOL) wait

run: demo

demo: up
	$(COMPOSE) --profile demo up -d --build --no-deps simulator

simulate: up
	$(COMPOSE) --profile demo run --rm --build --no-deps simulator -devices=$(DEVICES) -interval=$(INTERVAL) -rounds=$(ROUNDS) -seed=$(SEED)

wait: tools
	$(TOOL) wait

down:
	$(COMPOSE) --profile demo --profile tools down

stop:
	$(COMPOSE) --profile demo stop $(SERVICES)

restart: tools
	$(COMPOSE) --profile demo restart $(SERVICES)
	$(TOOL) wait

ps:
	$(COMPOSE) --profile demo ps

logs:
	$(COMPOSE) --profile demo logs --follow --tail=$(TAIL) $(SERVICES)

config:
	$(COMPOSE) --profile demo --profile tools config --quiet

fmt: tools
	$(COMPOSE) --profile tools run --rm --no-deps --user $(LOCAL_USER) --volume "$(CURDIR):/src" tools make -f build/Makefile fmt

test: tools
	$(TOOL) test

vet: tools
	$(TOOL) vet

check: tools
	$(TOOL) check

test-race: tools
	$(TOOL) test-race

test-integration: up
	$(TOOL) test-integration

smoke: up
	$(TOOL) smoke

recovery: up
	$(COMPOSE) --profile tools run --rm --no-deps recovery

verify: check test-race smoke recovery

devices: tools
	$(TOOL) devices

latest: tools
	$(TOOL) latest

history: tools
	$(TOOL) history
