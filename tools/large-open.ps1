param(
    [ValidateRange(1, 10)][int]$Runs = 3,
    [ValidateSet('xlsx', 'docx', 'pdf', 'json', 'html')][string[]]$Formats = @('xlsx', 'docx', 'pdf', 'json', 'html'),
    [ValidateRange(30, 360)][int]$TimeoutSeconds = 100
)

$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot -Parent
. (Join-Path $PSScriptRoot 'dev-env.ps1')
$go = Initialize-VivaDevEnvironment $project
& (Join-Path $PSScriptRoot 'web-assets.ps1')
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$results = Join-Path $project ('.dev\large-open-' + $stamp)
$fixtures = Join-Path $results 'fixtures'
New-Item -ItemType Directory -Path $fixtures | Out-Null
$exe = Join-Path $results 'viva-open.test.exe'
$oldFixtures = $env:VIVA_LARGE_FIXTURES
$oldFormat = $env:VIVA_LARGE_FORMAT
$oldProfile = $env:VIVA_LARGE_PROFILE

Push-Location -LiteralPath $project
try {
    & $go test -mod=vendor -c -o $exe ./internal/editor
    if ($LASTEXITCODE -ne 0) { throw 'Could not compile native timing tests.' }
    $env:VIVA_LARGE_FIXTURES = $fixtures
    & $exe '-test.run=^TestMakeLargeOpenFixtures$' '-test.v'
    if ($LASTEXITCODE -ne 0) { throw 'Could not generate timing fixtures.' }
    if (!('VivaIsolatedDesktop' -as [type])) {
        Add-Type -Path (Join-Path $PSScriptRoot 'IsolatedDesktop.cs')
    }

    $samples = [Collections.Generic.List[object]]::new()
    $failed = 0
    for ($run = 1; $run -le $Runs; $run++) {
        foreach ($format in $Formats) {
            $env:VIVA_LARGE_FORMAT = $format
            $env:VIVA_LARGE_PROFILE = Join-Path $results ("webview-$format-$run")
            $log = Join-Path $results ("$format-$run.log")
            $exitCode = [VivaIsolatedDesktop]::Run($exe, $project, $log, $TimeoutSeconds, 'TestNativeLargeOpen')
            $output = Get-Content -LiteralPath $log -Raw -Encoding UTF8
            $match = [regex]::Match($output, 'VIVA_OPEN (\w+) (\d+) (\d+) (editable|preview)')
            if ($exitCode -ne 0 -or !$match.Success) {
                $failed++
                Write-Host "$format run $run failed with code $exitCode. See $log"
                $output.TrimEnd()
                continue
            }
            $sample = [pscustomobject]@{
                Format = $match.Groups[1].Value
                Run = $run
                Bytes = [long]$match.Groups[2].Value
                Milliseconds = [long]$match.Groups[3].Value
                Milestone = $match.Groups[4].Value
            }
            $samples.Add($sample)
            Write-Host "$format run $run took $($sample.Milliseconds) ms"
        }
    }
    $samples | Export-Csv -LiteralPath (Join-Path $results 'results.csv') -NoTypeInformation -Encoding UTF8
    $samples | Format-Table -AutoSize
    Write-Host "Results saved in $results"
    if ($failed -gt 0) { throw "$failed native timing runs failed. Logs and completed samples are available." }
} finally {
    $env:VIVA_LARGE_FIXTURES = $oldFixtures
    $env:VIVA_LARGE_FORMAT = $oldFormat
    $env:VIVA_LARGE_PROFILE = $oldProfile
    Pop-Location
}
