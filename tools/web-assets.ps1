param([switch]$Force, [switch]$PublicRelease)

$ErrorActionPreference = 'Stop'
$project = (Resolve-Path -LiteralPath (Split-Path $PSScriptRoot -Parent)).ProviderPath
$frontend = Join-Path $project 'office-ui'
$assetRoot = Join-Path $project 'internal\editor\assets'
$officeAssetDirectory = if ($PublicRelease) { 'office-public' } else { 'office' }
$assets = Join-Path $assetRoot $officeAssetDirectory
$devDirectory = Join-Path $project '.dev'

function Install-CompressedOfficeAssets([string]$source) {
    if ((Resolve-Path -LiteralPath $source).ProviderPath -ne $source -or
        ((Get-Item -LiteralPath $source).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw 'Unexpected directory for compressed office assets.'
    }

    $packed = Join-Path $source 'packed'
    if (Test-Path -LiteralPath $packed -PathType Container) { return }

    $temporary = Join-Path $devDirectory ('office-packed-' + [Guid]::NewGuid().ToString('N'))
    [IO.Directory]::CreateDirectory($temporary) | Out-Null
    foreach ($file in Get-ChildItem -LiteralPath $source -Recurse -File) {
        $relative = [IO.Path]::GetRelativePath($source, $file.FullName)
        if (@(($relative -split '[\\/]') -match '^[._]').Count -ne 0) { continue }
        $destination = Join-Path $temporary $relative
        [IO.Directory]::CreateDirectory((Split-Path -Parent $destination)) | Out-Null
        # Page HTML stays available for the existing per-request theme injection.
        if ($file.Length -ge 1024 -and $file.Extension -ne '.html') {
            $gzipPath = $destination + '.gz'
            $output = [IO.File]::Create($gzipPath)
            try {
                $gzip = [IO.Compression.GZipStream]::new($output, [IO.Compression.CompressionLevel]::Optimal, $true)
                try {
                    $inputStream = $file.OpenRead()
                    try { $inputStream.CopyTo($gzip) } finally { $inputStream.Dispose() }
                } finally { $gzip.Dispose() }
            } finally { $output.Dispose() }
            if ((Get-Item -LiteralPath $gzipPath).Length -lt $file.Length) { continue }
            [IO.File]::Delete($gzipPath)
        }
        [IO.File]::Copy($file.FullName, $destination)
    }
    if ((Resolve-Path -LiteralPath $temporary).ProviderPath -ne $temporary -or
        ((Get-Item -LiteralPath $temporary).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        throw 'Unexpected compressed office asset stage.'
    }
    [IO.Directory]::Move($temporary, $packed)
}

$expected = @('main.js', 'xlsx.html', 'pdf.html', 'pdf-resources\pdf.worker.min.mjs', 'THIRD-PARTY-NOTICES.txt')
$buildOptions = @()
if ($PublicRelease) {
    $buildOptions += '--public-release'
    $Force = $Force -or (Test-Path -LiteralPath (Join-Path $assets 'docx.html'))
} else {
    $expected += 'docx.html'
}
$missing = @($expected | Where-Object { !(Test-Path -LiteralPath (Join-Path $assets $_) -PathType Leaf) })
if (!$Force -and $missing.Count -eq 0) {
    Install-CompressedOfficeAssets $assets
    return
}

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
    & $node build.mjs $stage @buildOptions
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

Install-CompressedOfficeAssets $stage

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
