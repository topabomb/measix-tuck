[CmdletBinding()]
param(
    [string[]]$Tool = @('grok-video'),
    [string[]]$Target = @(),
    [string]$Version = '0.1.0'
)
$ErrorActionPreference = 'Stop'
$toolRoot = $PSScriptRoot
$oldGOOS = $env:GOOS
$oldGOARCH = $env:GOARCH
$oldCGO = $env:CGO_ENABLED
if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9.+-]*$') { throw 'Version must contain only letters, digits, dot, plus or hyphen' }
try {
    $env:GOOS = $null
    $env:GOARCH = $null
    $env:CGO_ENABLED = '0'
    Push-Location (Join-Path $toolRoot 'common')
    try {
        go test ./...
        if ($LASTEXITCODE -ne 0) { throw 'Common library tests failed' }
        go vet ./...
        if ($LASTEXITCODE -ne 0) { throw 'Common library vet failed' }
    } finally { Pop-Location }
    if ($Target.Count -eq 0) {
        $platform = go env GOOS GOARCH
        if ($LASTEXITCODE -ne 0) { throw 'go env failed' }
        $Target = @("$($platform[0])-$($platform[1])")
    }
    foreach ($name in $Tool) {
        if ($name -notmatch '^[a-z][a-z0-9-]*$') { throw "Invalid tool name: $name" }
        $source = Join-Path $toolRoot $name
        if (-not (Test-Path -LiteralPath (Join-Path $source 'go.mod'))) { throw "Unknown Go tool: $name" }
        Push-Location $source
        try {
            $env:GOOS = $null
            $env:GOARCH = $null
            go test ./...
            if ($LASTEXITCODE -ne 0) { throw "Tests failed: $name" }
            go vet ./...
            if ($LASTEXITCODE -ne 0) { throw "Vet failed: $name" }
            foreach ($platform in $Target) {
                if ($platform -notmatch '^(windows|linux|darwin)-(amd64|arm64)$') { throw "Unsupported target: $platform" }
                $env:GOOS = $Matches[1]
                $env:GOARCH = $Matches[2]
                $directory = Join-Path $toolRoot "dist/$platform"
                New-Item -ItemType Directory -Force -Path $directory | Out-Null
                $binaryName = if ($env:GOOS -eq 'windows') { "$name.exe" } else { $name }
                $destination = Join-Path $directory $binaryName
                go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $destination .
                if ($LASTEXITCODE -ne 0) { throw "Build failed: $name ($platform)" }
                $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $destination).Hash.ToLowerInvariant()
                [IO.File]::WriteAllText("$destination.sha256", "$hash  $binaryName`n", [Text.UTF8Encoding]::new($false))
                Write-Host "Built $destination"
            }
        } finally { Pop-Location }
    }
} finally {
    $env:GOOS = $oldGOOS
    $env:GOARCH = $oldGOARCH
    $env:CGO_ENABLED = $oldCGO
}
