function Initialize-VivaDevEnvironment([string]$ProjectDirectory) {
    $devDirectory = Join-Path $ProjectDirectory '.dev'
    $env:GOPATH = Join-Path $devDirectory 'gopath'
    $env:GOMODCACHE = Join-Path $env:GOPATH 'pkg\mod'
    $env:GOCACHE = Join-Path $devDirectory 'gocache'
    $env:GOTMPDIR = Join-Path $devDirectory 'tmp'
    $env:TEMP = $env:GOTMPDIR
    $env:TMP = $env:GOTMPDIR
    $env:NPM_CONFIG_CACHE = Join-Path $devDirectory 'npm-cache'
    $env:GOTOOLCHAIN = 'local'
    $env:CGO_ENABLED = '0'
    $env:GOTELEMETRY = 'off'

    New-Item -ItemType Directory -Force -Path $env:GOPATH,$env:GOCACHE,$env:GOTMPDIR,$env:NPM_CONFIG_CACHE | Out-Null
    $portableGo = Join-Path $devDirectory 'go\bin\go.exe'
    if (Test-Path -LiteralPath $portableGo -PathType Leaf) {
        return $portableGo
    }
    $installedGo = Get-Command go.exe -ErrorAction SilentlyContinue
    if ($installedGo) {
        return $installedGo.Source
    }
    throw 'Go is required. Install Go or place the portable toolchain in .dev\go.'
}
