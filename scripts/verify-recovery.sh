#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
DOCKER=${DOCKER:-docker}
API_URL=${API_URL:-http://localhost:8080}
command -v curl >/dev/null
command -v jq >/dev/null
command -v "$DOCKER" >/dev/null

compose() { "$DOCKER" compose -f deploy/compose.yaml "$@"; }
api() { curl --fail --silent --show-error --connect-timeout 2 --max-time 5 "$@"; }

stopped=0
cleanup() {
    status=$?
    trap - EXIT
    if ((stopped)); then
        if ! compose start processor; then
            echo 'Failed to restore processor; run make up' >&2
            if ((status == 0)); then status=1; fi
        fi
    fi
    exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

device_id="recovery-$(cat /proc/sys/kernel/random/uuid)"
session_id=$(cat /proc/sys/kernel/random/uuid)
body=$(jq -nc --arg id "$device_id" \
    '{id:$id,name:"Recovery test",expected_interval_seconds:1}')
api -H 'Content-Type: application/json' --data-binary "$body" \
    "$API_URL/api/v1/devices" >/dev/null

# Set the cleanup flag first so an interrupted stop also restores the service.
stopped=1
compose stop processor
for sequence in 1 2 3; do
    timestamp=$(date -u +'%Y-%m-%dT%H:%M:%S.%NZ')
    payload=$(jq -nc --arg id "$device_id" --arg session "$session_id" \
        --arg timestamp "$timestamp" --argjson sequence "$sequence" \
        '{schema_version:1,message_id:("message-"+($sequence|tostring)),
          device_id:$id,session_id:$session,sequence:$sequence,timestamp:$timestamp,
          voltage_v:230,current_a:5,active_power_w:1092.5,power_factor:0.95,temperature_c:35}')
    printf '%s' "$payload" | compose exec -T mqtt mosquitto_pub \
        -h localhost -t "devices/$device_id/telemetry" -q 1 -s
done

history=$(api "$API_URL/api/v1/devices/$device_id/measurements")
if [[ $(jq -er '.items | length' <<<"$history") != 0 ]]; then
    echo 'Processor was expected to be stopped' >&2
    exit 1
fi
compose start processor
stopped=0

deadline=$((SECONDS + 60))
while :; do
    history=$(api "$API_URL/api/v1/devices/$device_id/measurements")
    count=$(jq -er '.items | length' <<<"$history")
    if [[ $count == 3 ]]; then break; fi
    if ((SECONDS >= deadline)); then
        echo "Expected 3 persisted records, got $count" >&2
        exit 1
    fi
    sleep 0.5
done
if ! jq -e '([.items[].measurement.sequence] | sort) == [1,2,3]' \
    <<<"$history" >/dev/null; then
    echo 'Unexpected sequences after recovery' >&2
    exit 1
fi
echo "Recovery PASS: device=$device_id; processor stopped during 3 publishes; 3 unique measurements visible after restart."
