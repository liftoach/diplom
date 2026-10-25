param([int]$TimeoutSeconds = 120)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
Push-Location $root
try {
    foreach ($port in 8080, 8081, 8082) {
        $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
        $ready = $false
        do {
            try {
                $response = Invoke-WebRequest "http://localhost:$port/health/ready" -TimeoutSec 3
                $ready = $response.StatusCode -eq 200
            } catch { $ready = $false }
            if (-not $ready) { Start-Sleep -Milliseconds 500 }
        } until ($ready -or (Get-Date) -ge $deadline)
        if (-not $ready) { throw "Service $port is not ready" }
    }
    $env:TEST_DATABASE_URL = 'postgres://telemetry:telemetry@localhost:5432/telemetry?sslmode=disable'
    go test -tags=integration -count=1 -v ./internal/storage ./tests/e2e
    if ($LASTEXITCODE -ne 0) { throw 'Integration tests failed' }
} finally { Pop-Location }
