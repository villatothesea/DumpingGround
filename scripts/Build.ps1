param([string]$Go = "go")
$ErrorActionPreference = 'Stop'
Push-Location "$PSScriptRoot\.."
try {
    & $Go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed' }
    & $Go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Static checks failed' }
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    New-Item -ItemType Directory -Force build | Out-Null
    & $Go build -tags mygo_noinspector -trimpath -ldflags '-s -w -H=windowsgui' -o build/DumpingGround.exe ./cmd/dumpingground
    if ($LASTEXITCODE -ne 0) { throw 'Build failed; close the running app before replacing it' }
    $hash = (Get-FileHash build/DumpingGround.exe -Algorithm SHA256).Hash.ToLower()
    "$hash  DumpingGround.exe" | Set-Content -Encoding ascii build/SHA256SUMS.txt
    Write-Host 'Ready: build/DumpingGround.exe'
} finally { Pop-Location }
