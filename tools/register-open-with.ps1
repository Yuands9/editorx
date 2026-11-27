param(
    [string]$Executable,
    [switch]$Unregister
)

$ErrorActionPreference = 'Stop'
if (!$Executable) {
    $Executable = Join-Path $PSScriptRoot 'EditorX.exe'
    if (!(Test-Path -LiteralPath $Executable -PathType Leaf)) {
        $Executable = Join-Path (Split-Path $PSScriptRoot -Parent) 'dist\EditorX.exe'
    }
}
$appKey = 'Applications\' + [IO.Path]::GetFileName($Executable)
$progId = 'EditorX.File'
$openVerb = '*\shell\EditorX.Open'
$extensions = @(
    '.txt', '.log', '.md', '.markdown', '.json', '.jsonl', '.ndjson', '.xml', '.xhtml', '.html', '.htm',
    '.yaml', '.yml', '.properties', '.ini', '.toml', '.env', '.cfg', '.conf', '.csv', '.tsv',
    '.go', '.js', '.ts', '.jsx', '.tsx', '.css', '.scss', '.svg', '.java', '.py', '.sql', '.ps1',
    '.c', '.cpp', '.h', '.hpp', '.cs', '.rs', '.sh', '.bat', '.cmd',
    '.docx', '.xlsx', '.pdf', '.xmind', '.png', '.jpg', '.jpeg', '.gif', '.bmp', '.webp', '.tif', '.tiff'
)

if (!$Unregister) {
    $exe = Get-Item -LiteralPath $Executable
    if ($exe.PSIsContainer -or $exe.Extension -ne '.exe') {
        throw '请选择要注册的 EditorX.exe 文件。'
    }
    $command = '"{0}" "%1"' -f $exe.FullName
    $icon = '"{0}",0' -f $exe.FullName
}

$classes = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey('Software\Classes')

function Set-ClassValue([string]$Path, [string]$Name, [string]$Value) {
    $key = $classes.CreateSubKey($Path)
    try {
        $key.SetValue($Name, $Value, [Microsoft.Win32.RegistryValueKind]::String)
    } finally {
        $key.Dispose()
    }
}

try {
    if ($Unregister) {
        foreach ($extension in $extensions) {
            $key = $classes.OpenSubKey("$extension\OpenWithProgids", $true)
            if ($null -ne $key) {
                try {
                    $key.DeleteValue($progId, $false)
                } finally {
                    $key.Dispose()
                }
            }
        }
        foreach ($key in $appKey, $progId, $openVerb) {
            $classes.DeleteSubKeyTree($key, $false)
        }
    } else {
        Set-ClassValue $appKey 'FriendlyAppName' 'EditorX'
        Set-ClassValue "$appKey\DefaultIcon" '' $icon
        Set-ClassValue "$appKey\shell\open\command" '' $command
        Set-ClassValue $progId '' 'EditorX 文档'
        Set-ClassValue "$progId\DefaultIcon" '' $icon
        Set-ClassValue "$progId\shell\open\command" '' $command
        foreach ($extension in $extensions) {
            Set-ClassValue "$appKey\SupportedTypes" $extension ''
            Set-ClassValue "$extension\OpenWithProgids" $progId ''
        }
        Set-ClassValue $openVerb '' '使用 EditorX 打开'
        Set-ClassValue $openVerb 'Icon' $icon
        Set-ClassValue "$openVerb\command" '' $command
    }
} finally {
    $classes.Dispose()
}

if (!('EditorXShellNotifications' -as [type])) {
    Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;

public static class EditorXShellNotifications {
    [DllImport("shell32.dll")]
    private static extern void SHChangeNotify(uint eventId, uint flags, IntPtr item1, IntPtr item2);

    public static void AssociationsChanged() {
        SHChangeNotify(0x08000000, 0, IntPtr.Zero, IntPtr.Zero);
    }
}
'@
}
[EditorXShellNotifications]::AssociationsChanged()

if ($Unregister) {
    Write-Output '已移除当前用户的 EditorX 打开方式。'
} else {
    Write-Output "已为当前用户注册打开方式：$($exe.FullName)"
}
