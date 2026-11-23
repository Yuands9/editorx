$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$assetDirectory = Join-Path (Split-Path $PSScriptRoot -Parent) 'internal\editor\assets'

function New-RoundedSquare([single]$origin, [single]$size, [single]$radius) {
    $path = [Drawing.Drawing2D.GraphicsPath]::new()
    $diameter = $radius * 2
    $opposite = $origin + $size - $diameter
    $path.AddArc($origin, $origin, $diameter, $diameter, 180, 90)
    $path.AddArc($opposite, $origin, $diameter, $diameter, 270, 90)
    $path.AddArc($opposite, $opposite, $diameter, $diameter, 0, 90)
    $path.AddArc($origin, $opposite, $diameter, $diameter, 90, 90)
    $path.CloseFigure()
    return $path
}

function New-Gradient([Drawing.PointF]$start, [Drawing.PointF]$end, [string]$from, [string]$to) {
    return [Drawing.Drawing2D.LinearGradientBrush]::new(
        $start, $end, [Drawing.ColorTranslator]::FromHtml($from), [Drawing.ColorTranslator]::FromHtml($to))
}

$bitmap = [Drawing.Bitmap]::new(1024, 1024, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
$graphics.ScaleTransform(2, 2)

$tile = New-RoundedSquare 28 456 116
$tileBrush = New-Gradient ([Drawing.PointF]::new(28, 28)) ([Drawing.PointF]::new(484, 484)) '#080B11' '#112540'
$edge = New-RoundedSquare 30 452 114
$border = [Drawing.Pen]::new([Drawing.ColorTranslator]::FromHtml('#264567'), 4)
$graphics.FillPath($tileBrush, $tile)
$graphics.DrawPath($border, $edge)

$backStart = [Drawing.PointF]::new(147, 147)
$backEnd = [Drawing.PointF]::new(365, 365)
$frontStart = [Drawing.PointF]::new(147, 365)
$frontEnd = [Drawing.PointF]::new(365, 147)
$backBrush = New-Gradient ([Drawing.PointF]::new(124, 124)) ([Drawing.PointF]::new(388, 388)) '#5B8DFF' '#1D48C8'
$frontBrush = New-Gradient ([Drawing.PointF]::new(124, 388)) ([Drawing.PointF]::new(388, 124)) '#3268FA' '#78D1FF'
$back = [Drawing.Pen]::new($backBrush, 62)
$front = [Drawing.Pen]::new($frontBrush, 62)
foreach ($stroke in @($back, $front)) {
    $stroke.StartCap = [Drawing.Drawing2D.LineCap]::Round
    $stroke.EndCap = [Drawing.Drawing2D.LineCap]::Round
}
$graphics.DrawLine($back, $backStart, $backEnd)
$graphics.DrawLine($front, $frontStart, $frontEnd)
$graphics.Dispose()

try {
    foreach ($size in @(512, 96, 32, 16)) {
        $output = [Drawing.Bitmap]::new($size, $size, [Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $resizer = [Drawing.Graphics]::FromImage($output)
        try {
            $resizer.InterpolationMode = [Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $resizer.DrawImage($bitmap, 0, 0, $size, $size)
            $name = if ($size -eq 512) {'icon.png'} else {"icon-$size.png"}
            $output.Save((Join-Path $assetDirectory $name), [Drawing.Imaging.ImageFormat]::Png)
        } finally {
            $resizer.Dispose()
            $output.Dispose()
        }
    }
} finally {
    foreach ($resource in @($back, $front, $border, $backBrush, $frontBrush, $tileBrush, $edge, $tile, $bitmap)) {
        $resource.Dispose()
    }
}
