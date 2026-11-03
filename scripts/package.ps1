[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$projectRoot = Split-Path -Parent $PSScriptRoot
& (Join-Path $projectRoot 'build.ps1')
$version = (Get-Content -LiteralPath (Join-Path $projectRoot 'VERSION') -Raw).Trim()
$outputDir = Join-Path $projectRoot 'dist'
$archive = Join-Path $outputDir "LoLSettings-v$version-windows-x64.zip"
# An explicit allowlist keeps profiles, credentials and preview binaries out of the release.
$releaseFiles = @(
    (Join-Path $outputDir 'LoLSettings.exe'),
    (Join-Path $projectRoot 'README.md')
)
$license = Join-Path $projectRoot 'LICENSE'
if (Test-Path -LiteralPath $license) { $releaseFiles += $license }
Compress-Archive -LiteralPath $releaseFiles -DestinationPath $archive -Force
Write-Host "Packaged $archive"
