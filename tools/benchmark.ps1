param([string]$Name = "optimized", [switch]$Baseline)
$ErrorActionPreference = "Stop"
$projectDirectory = Split-Path $PSScriptRoot -Parent
$devRoot = Join-Path $projectDirectory '.dev'
. (Join-Path $PSScriptRoot 'dev-env.ps1')
$go = Initialize-VivaDevEnvironment $projectDirectory
& (Join-Path $PSScriptRoot 'web-assets.ps1')
$arguments = @("test","-mod=vendor","-run","^$","-bench","BenchmarkInteractions","-benchmem","-benchtime=1s","-count=3")
if ($Baseline) {
    $replace = @{}
    Get-ChildItem "$devRoot/backups/v0.7.0" -Filter "*.go" | Where-Object {$_.Name -ne "code_editor.go"} | ForEach-Object {
        $replace[(Join-Path (Split-Path $devRoot -Parent) $_.Name)]=$_.FullName
    }
    $replace[(Join-Path (Split-Path $devRoot -Parent) "vendor/github.com/vivaui/viva/components/code_editor.go")]=(Join-Path $devRoot "backups/v0.7.0/code_editor.go")
    "package main" | Set-Content "$devRoot/empty_test.go"
    Get-ChildItem (Split-Path $devRoot -Parent) -Filter "*.go" | Where-Object { $_.Name -ne "performance_test.go" -and !$replace.ContainsKey($_.FullName) } | ForEach-Object {
        $replace[$_.FullName]=(Join-Path $devRoot "empty_test.go")
    }
    @{Replace=$replace} | ConvertTo-Json | Set-Content "$devRoot/baseline-overlay.json"
    $arguments += "-overlay=$devRoot/baseline-overlay.json"
}
Push-Location -LiteralPath $projectDirectory
try {
    & $go @arguments . | Tee-Object "$devRoot/$Name.txt"
    if ($LASTEXITCODE -ne 0) { throw "benchmark failed" }
} finally { Pop-Location }
