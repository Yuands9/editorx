param([string]$Name = "optimized", [switch]$Baseline)
$ErrorActionPreference = "Stop"
$projectDirectory = Split-Path $PSScriptRoot -Parent
$devRoot = Join-Path $projectDirectory '.dev'
. (Join-Path $PSScriptRoot 'dev-env.ps1')
$go = Initialize-VivaDevEnvironment $projectDirectory
& (Join-Path $PSScriptRoot 'web-assets.ps1')
$editorSource = Join-Path $projectDirectory 'internal\editor'
$arguments = @("test","-mod=vendor","-run","^$","-bench","BenchmarkInteractions","-benchmem","-benchtime=1s","-count=3")
if ($Baseline) {
    $baselineDirectory = Join-Path $devRoot 'backups\v0.7.0'
    $overlayDirectory = Join-Path $devRoot ('benchmark-baseline-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $overlayDirectory | Out-Null
    $replace = @{}
    foreach ($file in (Get-ChildItem -LiteralPath $baselineDirectory -File -Filter '*.go')) {
        if ($file.Name -eq 'code_editor.go') {
            $replace[(Join-Path $projectDirectory 'vendor/github.com/vivaui/viva/components/code_editor.go')] = $file.FullName
            continue
        }
        $name = if ($file.Name -eq 'main.go') { 'editor.go' } else { $file.Name }
        $target = Join-Path $overlayDirectory $name
        $source = [IO.File]::ReadAllText($file.FullName) -creplace '(?m)^package main\b', 'package editor'
        [IO.File]::WriteAllText($target, $source)
        $replace[(Join-Path $editorSource $name)] = $target
    }
    $empty = Join-Path $overlayDirectory 'empty.go'
    'package editor' | Set-Content -LiteralPath $empty
    foreach ($file in (Get-ChildItem -LiteralPath $editorSource -File -Filter '*.go')) {
        if ($file.Name -ne 'performance_test.go' -and !$replace.ContainsKey($file.FullName)) {
            $replace[$file.FullName] = $empty
        }
    }
    $manifest = Join-Path $overlayDirectory 'overlay.json'
    @{Replace = $replace} | ConvertTo-Json | Set-Content -LiteralPath $manifest
    $arguments += "-overlay=$manifest"
}
Push-Location -LiteralPath $projectDirectory
try {
    & $go @arguments ./internal/editor | Tee-Object "$devRoot/$Name.txt"
    if ($LASTEXITCODE -ne 0) { throw "benchmark failed" }
} finally { Pop-Location }
