param(
    [ValidateSet('build', 'test', 'preview', 'run')][string]$Action = 'build',
    [string]$OutputDirectory = (Join-Path $PSScriptRoot 'dist'),
    [switch]$RebuildAssets
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'tools\dev-env.ps1')
$go = Initialize-VivaDevEnvironment $PSScriptRoot
& (Join-Path $PSScriptRoot 'tools\web-assets.ps1') -Force:$RebuildAssets

$previousPreviewDirectory = $env:VIVA_PREVIEW_DIR
$output = [IO.Path]::GetFullPath($OutputDirectory)
Push-Location -LiteralPath $PSScriptRoot
try {
    switch ($Action) {
        'test' {
            & $go test -mod=vendor ./... ./vendor/github.com/vivaui/viva/components ./vendor/github.com/vivaui/viva/internal/platform/windows
        }
        'preview' {
            $env:VIVA_PREVIEW_DIR = Join-Path $PSScriptRoot '.dev\previews'
            New-Item -ItemType Directory -Path $env:VIVA_PREVIEW_DIR -Force | Out-Null
            & $go test -mod=vendor -run 'Test(RenderPreview|MediaPreviews|TabPreviews|SessionPreviews)' -count=1 -v .
        }
        default {
            New-Item -ItemType Directory -Path $output -Force | Out-Null
            $exe = Join-Path $output 'EditorX.exe'
            & $go build -mod=vendor -trimpath -ldflags '-s -w -H windowsgui' -o $exe .
            if ($LASTEXITCODE -ne 0) { throw 'Go build failed.' }
            & $go run -mod=vendor ./tools/package $exe
            if ($LASTEXITCODE -ne 0) { throw 'Windows packaging failed.' }

            $notices = @{
                'assets/office/THIRD-PARTY-NOTICES.txt' = 'THIRD-PARTY-NOTICES.txt'
                'assets/ai-providers/LICENSE.txt' = 'AI-PROVIDER-LICENSE.txt'
                'assets/ai-providers/DBX-LICENSE.txt' = 'DBX-ICON-LICENSE.txt'
                'assets/terminal/XTERM-LICENSE.txt' = 'XTERM-LICENSE.txt'
                'assets/terminal/ADDON-FIT-LICENSE.txt' = 'XTERM-ADDON-FIT-LICENSE.txt'
            }
            foreach ($source in $notices.Keys) {
                Copy-Item -LiteralPath $source -Destination (Join-Path $output $notices[$source]) -Force
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
