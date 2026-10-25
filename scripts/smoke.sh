#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
GO=${GO:-go}
command -v "$GO" >/dev/null
bash scripts/wait.sh

export TEST_DATABASE_URL=${TEST_DATABASE_URL:-postgres://telemetry:telemetry@localhost:5432/telemetry?sslmode=disable}
make -f build/Makefile test-integration GO="$GO"
