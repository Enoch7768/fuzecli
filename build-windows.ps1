$ErrorActionPreference = 'Stop'
$env:CGO_ENABLED = '0'
New-Item -ItemType Directory -Force -Path dist | Out-Null
$version = 'dev'
if ($env:FUZECLI_VERSION) { $version = $env:FUZECLI_VERSION }
go test ./...
go build -trimpath -ldflags "-s -w -X main.version=$version" -o dist/aicli.exe ./cmd/aicli
Write-Host "Built dist/aicli.exe"
