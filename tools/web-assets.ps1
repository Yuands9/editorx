param([switch]$Force)

$ErrorActionPreference = 'Stop'
$project = (Resolve-Path -LiteralPath (Split-Path $PSScriptRoot -Parent)).ProviderPath
$frontend = Join-Path $project 'office-ui'
$assetRoot = Join-Path $project 'internal\editor\assets'
$assets = Join-Path $assetRoot 'office'
$expected = @('main.js', 'xlsx.html', 'docx.html', 'pdf.html', 'pdf-resources\pdf.worker.min.mjs', 'THIRD-PARTY-NOTICES.txt')
$missing = @($expected | Where-Object { !(Test-Path -LiteralPath (Join-Path $assets $_) -PathType Leaf) })
if (!$Force -and $missing.Count -eq 0) { return }

$devDirectory = Join-Path $project '.dev'
$env:NPM_CONFIG_CACHE = Join-Path $devDirectory 'npm-cache'
$env:TEMP = Join-Path $devDirectory 'tmp'
$env:TMP = $env:TEMP
New-Item -ItemType Directory -Force -Path $env:NPM_CONFIG_CACHE,$env:TEMP | Out-Null
$suffix = [Guid]::NewGuid().ToString('N')
$stage = Join-Path $devDirectory ('office-web-stage-' + $suffix)
$backup = Join-Path $devDirectory ('office-web-backup-' + $suffix)

Push-Location -LiteralPath $frontend
try {
    if ($Force -or !(Test-Path -LiteralPath 'node_modules\esbuild\package.json' -PathType Leaf)) {
        $npm = (Get-Command npm.cmd).Source
        & $npm ci --prefer-offline --no-audit --no-fund
        if ($LASTEXITCODE -ne 0) { throw 'Installing locked web dependencies failed.' }
    }
    $node = (Get-Command node.exe).Source
    & $node build.mjs $stage
    if ($LASTEXITCODE -ne 0) { throw 'Building the embedded web assets failed.' }
} finally {
    Pop-Location
}

if ((Resolve-Path -LiteralPath $stage).ProviderPath -ne $stage -or
    (Resolve-Path -LiteralPath $assetRoot).ProviderPath -ne $assetRoot -or
    ((Get-Item -LiteralPath $assetRoot).Attributes -band [IO.FileAttributes]::ReparsePoint) -or
    ((Get-Item -LiteralPath $stage).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
    throw 'Unexpected web asset directory.'
}
foreach ($name in $expected) {
    if (!(Test-Path -LiteralPath (Join-Path $stage $name) -PathType Leaf)) {
        throw "The frontend build did not produce $name."
    }
}

if (Test-Path -LiteralPath $assets) {
    if ((Resolve-Path -LiteralPath $assets).ProviderPath -ne $assets -or
        ((Get-Item -LiteralPath $assets).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw 'Unexpected installed web asset directory.'
    }
    Move-Item -LiteralPath $assets -Destination $backup
}
try {
    Move-Item -LiteralPath $stage -Destination $assets
} catch {
    if (Test-Path -LiteralPath $backup) {
        Move-Item -LiteralPath $backup -Destination $assets
    }
    throw
}
