param(
    [int]$TimeoutSeconds = 50,
    [ValidateSet('TestNativeHTMLStartupTiming', 'TestNativeHTMLPreview')]
    [string]$TestName = 'TestNativeHTMLStartupTiming'
)

$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'native-smoke.ps1') -TimeoutSeconds $TimeoutSeconds -TestName $TestName -LogName 'html-timing.log'
