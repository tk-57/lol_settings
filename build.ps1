[CmdletBinding()]
param(
    [switch]$Preview
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = $PSScriptRoot
$version = (Get-Content -LiteralPath (Join-Path $projectRoot 'VERSION') -Raw).Trim()
if ($version -notmatch '^\d+\.\d+(\.\d+)?$') { throw 'VERSION must contain a numeric release version.' }
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go 1.26 or newer is required to build.' }
if ($env:OS -ne 'Windows_NT') { throw 'Use Windows to build and test this application.' }

$savedEnvironment = @{}
foreach ($key in @('CGO_ENABLED','GOOS','GOARCH','TMP','TEMP')) {
    $savedEnvironment[$key] = [Environment]::GetEnvironmentVariable($key, 'Process')
}
$buildTemp = Join-Path $projectRoot '.test-tmp'
$outputDir = Join-Path $projectRoot 'dist'
New-Item -ItemType Directory -Force -Path $buildTemp, $outputDir | Out-Null
Push-Location $projectRoot
try {
    $env:CGO_ENABLED = '0'
    $env:GOOS = 'windows'
    $env:GOARCH = 'amd64'
    $env:TMP = $buildTemp
    $env:TEMP = $buildTemp

    $unformatted = @(gofmt -l cmd internal)
    if ($LASTEXITCODE -ne 0 -or $unformatted.Count -gt 0) { throw "Run gofmt before building: $unformatted" }
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw 'Tests failed.' }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { throw 'Static checks failed.' }

    $flags = "-s -w -H windowsgui -X main.version=$version"
    $fileName = 'LoLSettings.exe'
    if ($Preview) {
        $flags += ' -X main.previewMode=true'
        $fileName = 'LoLSettings-Preview.exe'
    }
    $output = Join-Path $outputDir $fileName
    go build -trimpath -buildvcs=false -ldflags $flags -o $output ./cmd/lolsettings
    if ($LASTEXITCODE -ne 0) { throw 'Build failed. Close any running copy of the output executable and retry.' }
    Write-Host "Built $output (v$version, Windows x64)."
} finally {
    Pop-Location
    foreach ($key in $savedEnvironment.Keys) {
        [Environment]::SetEnvironmentVariable($key, $savedEnvironment[$key], 'Process')
    }
    if (Test-Path -LiteralPath $buildTemp) {
        if (@(Get-ChildItem -LiteralPath $buildTemp -Force).Count -eq 0) {
            [IO.Directory]::Delete($buildTemp)
        }
    }
}
