#!/usr/bin/env bash
set -euo pipefail

timeout=${TIMEOUT_SECONDS:-120}
if [[ ! $timeout =~ ^[1-9][0-9]*$ ]]; then
    echo 'TIMEOUT_SECONDS must be a positive integer' >&2
    exit 1
fi

for url in "${API_URL:-http://localhost:8080}" \
    "${INGESTION_URL:-http://localhost:8081}" \
    "${PROCESSOR_URL:-http://localhost:8082}"; do
    deadline=$((SECONDS + timeout))
    until curl --fail --silent --output /dev/null --connect-timeout 1 --max-time 3 \
        "$url/health/ready"; do
        if ((SECONDS >= deadline)); then
            echo "Service $url is not ready after ${timeout}s" >&2
            exit 1
        fi
        sleep 0.5
    done
    echo "Ready: $url"
done
