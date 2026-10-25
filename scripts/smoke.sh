#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
GO=${GO:-go}
timeout=${TIMEOUT_SECONDS:-120}
if [[ ! $timeout =~ ^[1-9][0-9]*$ ]]; then
    echo 'TIMEOUT_SECONDS must be a positive integer' >&2
    exit 1
fi
command -v curl >/dev/null
command -v "$GO" >/dev/null

for port in 8080 8081 8082; do
    deadline=$((SECONDS + timeout))
    until curl --fail --silent --output /dev/null --connect-timeout 1 --max-time 3 \
        "http://localhost:$port/health/ready"; do
        if ((SECONDS >= deadline)); then
            echo "Service $port is not ready" >&2
            exit 1
        fi
        sleep 0.5
    done
done

export TEST_DATABASE_URL=${TEST_DATABASE_URL:-postgres://telemetry:telemetry@localhost:5432/telemetry?sslmode=disable}
"$GO" test -tags=integration -count=1 -v ./internal/storage ./tests/e2e
