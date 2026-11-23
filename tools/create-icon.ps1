$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName PresentationCore
$assetDirectory = Join-Path (Split-Path $PSScriptRoot -Parent) 'internal\editor\assets'

[xml]$artwork = Get-Content -LiteralPath (Join-Path $assetDirectory 'icon.svg') -Raw
$tilePath = $artwork.svg.path | Where-Object { $_.id -eq 'tile' }
$markPath = $artwork.svg.path | Where-Object { $_.id -eq 'mark' }
$tile = [Windows.Media.Geometry]::Parse($tilePath.d)
$mark = [Windows.Media.Geometry]::Parse($markPath.d)

$themes = @(
    @{ Suffix = ''; Tile = $tilePath.fill; Mark = $markPath.fill },
    @{ Suffix = '-light'; Tile = '#F2F0EC'; Mark = '#0D0D0D' }
)
foreach ($theme in $themes) {
    $tileBrush = [Windows.Media.SolidColorBrush]::new([Windows.Media.ColorConverter]::ConvertFromString($theme.Tile))
    $markBrush = [Windows.Media.SolidColorBrush]::new([Windows.Media.ColorConverter]::ConvertFromString($theme.Mark))
    foreach ($size in @(512, 96, 32, 16)) {
        $visual = [Windows.Media.DrawingVisual]::new()
        $drawing = $visual.RenderOpen()
        try {
            $scale = $size / [double]$artwork.svg.width
            $drawing.PushTransform([Windows.Media.ScaleTransform]::new($scale, $scale))
            $drawing.DrawGeometry($tileBrush, $null, $tile)
            $drawing.DrawGeometry($markBrush, $null, $mark)
        } finally {
            $drawing.Close()
        }
        $bitmap = [Windows.Media.Imaging.RenderTargetBitmap]::new($size, $size, 96, 96, [Windows.Media.PixelFormats]::Pbgra32)
        $bitmap.Render($visual)
        $encoder = [Windows.Media.Imaging.PngBitmapEncoder]::new()
        $encoder.Frames.Add([Windows.Media.Imaging.BitmapFrame]::Create($bitmap))
        $stem = if ($size -eq 512) { 'icon' } else { "icon-$size" }
        $output = [IO.File]::Create((Join-Path $assetDirectory "$stem$($theme.Suffix).png"))
        try {
            $encoder.Save($output)
        } finally {
            $output.Dispose()
        }
    }
}
