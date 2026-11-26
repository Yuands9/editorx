param(
    [ValidateSet('build', 'test', 'preview', 'run')][string]$Action = 'build',
    [string]$OutputDirectory = (Join-Path $PSScriptRoot 'dist'),
    [switch]$RebuildAssets,
    [switch]$PublicRelease
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'tools\dev-env.ps1')
$go = Initialize-VivaDevEnvironment $PSScriptRoot
& (Join-Path $PSScriptRoot 'tools\web-assets.ps1') -Force:$RebuildAssets -PublicRelease:$PublicRelease
$goOptions = @('-mod=vendor')
$officeAssetDirectory = 'office'
if ($PublicRelease) {
    $goOptions += @('-tags', 'editorx_public')
    $officeAssetDirectory = 'office-public'
}

$previousPreviewDirectory = $env:VIVA_PREVIEW_DIR
$output = [IO.Path]::GetFullPath($OutputDirectory)
Push-Location -LiteralPath $PSScriptRoot
try {
    switch ($Action) {
        'test' {
            & $go test @goOptions ./... ./vendor/github.com/vivaui/viva/components ./vendor/github.com/vivaui/viva/internal/platform/windows
        }
        'preview' {
            $env:VIVA_PREVIEW_DIR = Join-Path $PSScriptRoot '.dev\previews'
            New-Item -ItemType Directory -Path $env:VIVA_PREVIEW_DIR -Force | Out-Null
            & $go test @goOptions -run 'Test(RenderPreview|MediaPreviews|TabPreviews|SessionPreviews)' -count=1 -v ./internal/editor
        }
        default {
            New-Item -ItemType Directory -Path $output -Force | Out-Null
            $exe = Join-Path $output 'EditorX.exe'
            $buildDate = Get-Date -Format 'yyyy-MM-dd'
            & $go build @goOptions -trimpath -ldflags "-s -w -H windowsgui -X github.com/Yuands9/editorx/internal/version.BuildDate=$buildDate" -o $exe ./cmd/editorx
            if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
            & $go run -mod=vendor ./tools/package $exe
            if ($LASTEXITCODE -ne 0) { throw 'Windows packaging failed.' }

            $notices = @{
                "$officeAssetDirectory/THIRD-PARTY-NOTICES.txt" = 'THIRD-PARTY-NOTICES.txt'
                'ai-providers/LICENSE.txt' = 'AI-PROVIDER-LICENSE.txt'
                'ai-providers/DBX-LICENSE.txt' = 'DBX-ICON-LICENSE.txt'
                'terminal/XTERM-LICENSE.txt' = 'XTERM-LICENSE.txt'
                'terminal/ADDON-FIT-LICENSE.txt' = 'XTERM-ADDON-FIT-LICENSE.txt'
            }
            $assets = Join-Path $PSScriptRoot 'internal\editor\assets'
            foreach ($source in $notices.Keys) {
                Copy-Item -LiteralPath (Join-Path $assets $source) -Destination (Join-Path $output $notices[$source]) -Force
            }
            if ($Action -eq 'run') {
                Start-Process -FilePath $exe -WorkingDirectory $PSScriptRoot -WindowStyle Normal
            }
        }
    }
    if ($LASTEXITCODE -ne 0) { throw "Go $Action failed." }
} finally {
    $env:VIVA_PREVIEW_DIR = $previousPreviewDirectory
    Pop-Location
}
