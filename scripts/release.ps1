param([string]$Version = '1.0.1')
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
Push-Location $repo
try {
    node scripts/build-web.cjs
    if ($LASTEXITCODE -ne 0) { throw 'Web build failed' }
    $package = Join-Path $repo "dist/ProjectBoard-$Version-windows-amd64"
    New-Item -ItemType Directory -Force -Path $package | Out-Null
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    go build -buildvcs=false -trimpath -ldflags '-s -w' -o (Join-Path $package 'projectboard.exe') ./cmd/projectboard
    if ($LASTEXITCODE -ne 0) { throw 'Go build failed' }
    Copy-Item -LiteralPath README.md -Destination (Join-Path $package 'README.md')
    Copy-Item -LiteralPath THIRD_PARTY_NOTICES.txt -Destination (Join-Path $package 'THIRD_PARTY_NOTICES.txt')
    $docsTarget = Join-Path $package 'docs'
    New-Item -ItemType Directory -Force -Path $docsTarget | Out-Null
    Get-ChildItem -LiteralPath docs -File | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $docsTarget -Force }
    Compress-Archive -LiteralPath $package -DestinationPath "$package.zip" -Force
    (Get-FileHash -LiteralPath "$package.zip" -Algorithm SHA256).Hash | Set-Content -LiteralPath "$package.zip.sha256"
    Get-Item -LiteralPath "$package.zip"
} finally { Pop-Location }
