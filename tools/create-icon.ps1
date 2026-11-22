$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$assetDirectory = Join-Path (Split-Path $PSScriptRoot -Parent) 'assets'
$bitmap = [Drawing.Bitmap]::new(1024,1024)
$graphics = [Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [Drawing.Drawing2D.SmoothingMode]::AntiAlias
$graphics.ScaleTransform(2,2)
function RoundedPath([single]$x,[single]$y,[single]$width,[single]$radius) {
    $path = [Drawing.Drawing2D.GraphicsPath]::new()
    $diameter = 2*$radius
    $path.AddArc($x,$y,$diameter,$diameter,180,90)
    $path.AddArc($x+$width-$diameter,$y,$diameter,$diameter,270,90)
    $path.AddArc($x+$width-$diameter,$y+$width-$diameter,$diameter,$diameter,0,90)
    $path.AddArc($x,$y+$width-$diameter,$diameter,$diameter,90,90)
    $path.CloseFigure()
    return $path
}
$tile = RoundedPath 28 28 456 116
$blue = [Drawing.Drawing2D.LinearGradientBrush]::new(
    [Drawing.PointF]::new(28,28), [Drawing.PointF]::new(484,484),
    [Drawing.ColorTranslator]::FromHtml('#548dff'), [Drawing.ColorTranslator]::FromHtml('#4145cc'))
$graphics.FillPath($blue,$tile)
$edge = RoundedPath 29 29 454 115
$border = [Drawing.Pen]::new([Drawing.Color]::FromArgb(41,255,255,255),2)
$graphics.DrawPath($border,$edge)
$ribbon = [Drawing.Drawing2D.GraphicsPath]::new()
$ribbon.AddLine(146,142,177,142)
$ribbon.AddBezier(177,142,184.333,142,189.667,145.667,193,153)
$ribbon.AddLine(193,153,256,300)
$ribbon.AddLine(256,300,319,153)
$ribbon.AddBezier(319,153,322.333,145.667,327.667,142,335,142)
$ribbon.AddLine(335,142,366,142)
$ribbon.AddBezier(366,142,377.333,142,380.667,147.333,376,158)
$ribbon.AddLine(376,158,283,361)
$ribbon.AddBezier(283,361,278.333,371.667,269.333,377,256,377)
$ribbon.AddBezier(256,377,242.667,377,233.667,371.667,229,361)
$ribbon.AddLine(229,361,136,158)
$ribbon.AddBezier(136,158,131.333,147.333,134.667,142,146,142)
$ribbon.CloseFigure()
$white = [Drawing.Drawing2D.LinearGradientBrush]::new(
    [Drawing.PointF]::new(136,142),[Drawing.PointF]::new(376,377),
    [Drawing.Color]::White,[Drawing.ColorTranslator]::FromHtml('#d9f2ff'))
$graphics.FillPath($white,$ribbon)
$right = [Drawing.Drawing2D.GraphicsPath]::new()
$right.AddLine(256,300,319,153)
$right.AddBezier(319,153,322.333,145.667,327.667,142,335,142)
$right.AddLine(335,142,366,142)
$right.AddBezier(366,142,377.333,142,380.667,147.333,376,158)
$right.AddLine(376,158,283,361)
$right.AddBezier(283,361,278.333,371.667,269.333,377,256,377)
$right.AddBezier(256,377,249.333,377,243.667,375.667,239,373)
$right.CloseFigure()
$cyan = [Drawing.SolidBrush]::new([Drawing.ColorTranslator]::FromHtml('#91daff'))
$graphics.FillPath($cyan,$right)
$graphics.Dispose()
foreach ($size in @(512,96,32,16)) {
    $output = [Drawing.Bitmap]::new($size,$size)
    $resizer = [Drawing.Graphics]::FromImage($output)
    $resizer.InterpolationMode=[Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $resizer.DrawImage($bitmap,0,0,$size,$size)
    $name = if ($size -eq 512) {'icon.png'} else {"icon-$size.png"}
    $output.Save((Join-Path $assetDirectory $name),[Drawing.Imaging.ImageFormat]::Png)
    $resizer.Dispose(); $output.Dispose()
}
$bitmap.Dispose(); $tile.Dispose(); $edge.Dispose(); $blue.Dispose(); $border.Dispose(); $ribbon.Dispose(); $white.Dispose(); $right.Dispose(); $cyan.Dispose()
