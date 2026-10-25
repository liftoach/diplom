# Commands run inside pinned Go/Docker images, called by the root Makefile.
.DEFAULT_GOAL := check
GO ?= go
SERVICE ?= api
API_URL ?= http://localhost:8080
DEVICE_ID ?= meter-000001

.PHONY: build-service fmt test vet check test-race test-integration wait smoke recovery devices latest history
.NOTPARALLEL:

build-service:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags="-s -w" -o /out/service ./cmd/$(SERVICE)

fmt:
	gofmt -w cmd internal tests

test:
	$(GO) test -count=1 ./...

vet:
	$(GO) vet ./...

check: test vet

test-race:
	$(GO) test -p 2 -race -count=1 ./...
	$(GO) vet ./...

test-integration:
	$(GO) test -tags=integration -count=1 -v ./internal/storage ./tests/e2e

wait:
	bash scripts/wait.sh

smoke:
	bash scripts/smoke.sh

recovery:
	bash scripts/verify-recovery.sh

devices latest history: SHELL := /bin/bash
devices latest history: .SHELLFLAGS := -eu -o pipefail -c

devices:
	curl --fail --silent --show-error --max-time 10 "$(API_URL)/api/v1/devices" | jq

latest:
	curl --fail --silent --show-error --max-time 10 "$(API_URL)/api/v1/devices/$(DEVICE_ID)/latest" | jq

history:
	curl --fail --silent --show-error --max-time 10 "$(API_URL)/api/v1/devices/$(DEVICE_ID)/measurements" | jq
