$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $root
$stopped = $false
try {
    $deviceId = 'recovery-' + [guid]::NewGuid().ToString('N')
    $body = @{ id = $deviceId; name = 'Recovery test'; expected_interval_seconds = 1 } | ConvertTo-Json -Compress
    $null = Invoke-RestMethod 'http://localhost:8080/api/v1/devices' -Method Post -ContentType 'application/json' -Body $body
    docker compose -f deploy/compose.yaml stop processor
    if ($LASTEXITCODE -ne 0) { throw 'Cannot stop processor' }
    $stopped = $true
    $sessionId = [guid]::NewGuid().ToString('N')
    foreach ($sequence in 1..3) {
        $payload = @{
            schema_version = 1
            message_id = 'message-' + $sequence
            device_id = $deviceId
            session_id = $sessionId
            sequence = $sequence
            timestamp = [DateTime]::UtcNow.ToString('o')
            voltage_v = 230
            current_a = 5
            active_power_w = 1092.5
            power_factor = 0.95
            temperature_c = 35
        } | ConvertTo-Json -Compress
        $payload | docker compose -f deploy/compose.yaml exec -T mqtt mosquitto_pub -h localhost -t "devices/$deviceId/telemetry" -q 1 -s
        if ($LASTEXITCODE -ne 0) { throw 'MQTT publish failed' }
    }
    $empty = Invoke-RestMethod "http://localhost:8080/api/v1/devices/$deviceId/measurements"
    if ($empty.items.Count -ne 0) { throw 'Processor was expected to be stopped' }
    docker compose -f deploy/compose.yaml start processor
    if ($LASTEXITCODE -ne 0) { throw 'Cannot start processor' }
    $stopped = $false
    $deadline = (Get-Date).AddSeconds(60)
    do {
        $history = Invoke-RestMethod "http://localhost:8080/api/v1/devices/$deviceId/measurements"
        if ($history.items.Count -eq 3) { break }
        Start-Sleep -Milliseconds 500
    } until ((Get-Date) -ge $deadline)
    if ($history.items.Count -ne 3) { throw "Expected 3 persisted records, got $($history.items.Count)" }
    $sequences = @($history.items | ForEach-Object { $_.measurement.sequence } | Sort-Object)
    if (($sequences -join ',') -ne '1,2,3') { throw 'Unexpected sequences after recovery' }
    Write-Output "Recovery PASS: device=$deviceId; processor stopped during 3 publishes; 3 unique measurements visible after restart."
} finally {
    if ($stopped) { docker compose -f deploy/compose.yaml start processor }
    Pop-Location
}
