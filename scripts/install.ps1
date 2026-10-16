$ErrorActionPreference = "Stop"

$repo = "Enoch7768/fuzecli"
$asset = "aicli-windows-amd64.exe"
$checksumAsset = "$asset.sha256"
$installDir = Join-Path $env:LOCALAPPDATA "FuzeCLI\bin"
$baseUrl = "https://github.com/$repo/releases/latest/download"
$exePath = Join-Path $installDir "aicli.exe"
$tmpExe = Join-Path $env:TEMP ([guid]::NewGuid().ToString() + ".exe")
$tmpChecksum = Join-Path $env:TEMP ([guid]::NewGuid().ToString() + ".sha256")

try {
    Invoke-WebRequest -Uri "$baseUrl/$asset" -OutFile $tmpExe
    Invoke-WebRequest -Uri "$baseUrl/$checksumAsset" -OutFile $tmpChecksum
    $expected = ((Get-Content -Raw $tmpChecksum).Trim() -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 $tmpExe).Hash.ToLowerInvariant()
    if ($expected -notmatch '^[0-9a-f]{64}$' -or $actual -ne $expected) {
        throw "SHA256 verification failed. The downloaded executable was not installed."
    }

    New-Item -ItemType Directory -Force -Path $installDir | Out-Null
    Move-Item -Force $tmpExe $exePath
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    $entries = @($userPath -split ';' | Where-Object { $_ })
    if ($entries -notcontains $installDir) {
        [Environment]::SetEnvironmentVariable("Path", (($entries + $installDir) -join ';'), "User")
        Write-Host "Added $installDir to your user PATH. Open a new terminal to use aicli."
    }
    & $exePath version
    Write-Host "Installed FuzeCLI to $exePath"
}
finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $tmpExe, $tmpChecksum
}
