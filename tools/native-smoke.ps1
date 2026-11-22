param([int]$TimeoutSeconds = 50, [ValidateSet('TestNativeHTMLPreview','TestNativeOfficeEditing','TestNativeLongHTMLText','TestNativeHTMLStartupTiming','TestNativeSearchDrag','TestNativeAIEnter','TestNativeTabRenameEnter','TestNativeOpenTiming')][string]$TestName = 'TestNativeHTMLPreview', [ValidateSet('1','2')][string]$Scale = '1', [ValidateSet('native-smoke.log', 'html-timing.log')][string]$LogName = 'native-smoke.log')
$ErrorActionPreference = 'Stop'
$projectDirectory = Split-Path $PSScriptRoot -Parent
$devDirectory = Join-Path $projectDirectory '.dev'
. (Join-Path $PSScriptRoot 'dev-env.ps1')
$go = Initialize-VivaDevEnvironment $projectDirectory
& (Join-Path $PSScriptRoot 'web-assets.ps1')
$previousNativeSmoke = $env:VIVA_NATIVE_SMOKE
$previousNativeDrag = $env:VIVA_NATIVE_DRAG
$previousScale = $env:VIVA_SCALE
$previousOpenProfile = $env:VIVA_NATIVE_OPEN_PROFILE
$openProfile = $null
if ($TestName -eq 'TestNativeOpenTiming') {
    # WebView2 can finish writing metrics just after the test process closes.
    $openProfile = Join-Path $env:GOTMPDIR ('viva-open-profile-' + [Guid]::NewGuid().ToString('N'))
    $env:VIVA_NATIVE_OPEN_PROFILE = $openProfile
}
$env:VIVA_NATIVE_SMOKE = '1'
if ($TestName -eq 'TestNativeSearchDrag') {
    $env:VIVA_NATIVE_DRAG = '1'
    $env:VIVA_SCALE = $Scale
}
$testExecutable = Join-Path $devDirectory 'viva-native.test.exe'
$logPath = Join-Path $devDirectory $LogName
Push-Location $projectDirectory
try {
    & $go test -mod=vendor -c -o $testExecutable .
    if ($LASTEXITCODE -ne 0) { throw 'Native test build failed' }
    if (!('VivaIsolatedDesktop' -as [type])) {
        Add-Type -Path (Join-Path $PSScriptRoot 'IsolatedDesktop.cs')
    }
    $nativeExit = [VivaIsolatedDesktop]::Run($testExecutable,$projectDirectory,$logPath,$TimeoutSeconds,$TestName)
    Get-Content -LiteralPath $logPath -Tail 65
    if ($nativeExit -ne 0) { throw "Native test failed: $nativeExit" }
} finally {
    $env:VIVA_NATIVE_SMOKE = $previousNativeSmoke
    $env:VIVA_NATIVE_DRAG = $previousNativeDrag
    $env:VIVA_SCALE = $previousScale
    $env:VIVA_NATIVE_OPEN_PROFILE = $previousOpenProfile
    if ($openProfile -and (Test-Path -LiteralPath $openProfile -PathType Container)) {
        $target = (Resolve-Path -LiteralPath $openProfile).ProviderPath
        $tempRoot = (Resolve-Path -LiteralPath $env:GOTMPDIR).ProviderPath
        if ($target -eq $openProfile -and (Split-Path -Parent $target) -eq $tempRoot -and
            !((Get-Item -LiteralPath $target).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            Remove-Item -LiteralPath $target -Recurse -ErrorAction SilentlyContinue
        }
    }
    Pop-Location
}
