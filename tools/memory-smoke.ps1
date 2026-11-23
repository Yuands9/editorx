param([string]$SourceDirectory = (Split-Path $PSScriptRoot -Parent), [string]$Label = 'candidate', [int]$Cycles = 6, [int]$TimeoutSeconds = 210, [string]$ExecutablePath = '', [string[]]$Files = @(), [int]$ObserveSeconds = 12)
$ErrorActionPreference = 'Stop'
$projectDirectory = Split-Path $PSScriptRoot -Parent
$devDirectory = Join-Path $projectDirectory '.dev'
. (Join-Path $PSScriptRoot 'dev-env.ps1')
$go = Initialize-VivaDevEnvironment $projectDirectory
$outputDirectory = Join-Path $devDirectory ('memory-' + $Label + '-' + (Get-Date -Format 'yyyyMMdd-HHmmss'))
New-Item -ItemType Directory -Path $outputDirectory | Out-Null
$env:VIVA_MEMORY_OUTPUT = $outputDirectory
$env:VIVA_MEMORY_CYCLES = [string]$Cycles
$env:VIVA_EDITOR_DATA_DIR = Join-Path $outputDirectory 'data'
$env:WEBVIEW2_USER_DATA_FOLDER = Join-Path $outputDirectory 'WebView2'
$testExecutable = Join-Path $outputDirectory 'viva-memory.test.exe'
$logPath = Join-Path $outputDirectory 'test.log'
$SourceDirectory = (Resolve-Path -LiteralPath $SourceDirectory).Path
Push-Location $SourceDirectory
try {
    $arguments = '-test.run ^TestNativeMemoryStress$ -test.v -test.timeout ' + ($TimeoutSeconds-5) + 's'
    if ($ExecutablePath) {
        $testExecutable = (Resolve-Path -LiteralPath $ExecutablePath).Path
        $arguments = ($Files | ForEach-Object { '"' + (Resolve-Path -LiteralPath $_).Path + '"' }) -join ' '
    } else {
        $editorPackage = if (Test-Path -LiteralPath 'internal\editor\editor.go' -PathType Leaf) {
            './internal/editor'
        } else {
            '.'
        }
        & $go test -mod=vendor -c -o $testExecutable $editorPackage
        if ($LASTEXITCODE -ne 0) { throw 'Memory test build failed' }
    }
    Add-Type -TypeDefinition @"
using System;
using System.IO;
using System.Text;
using System.Collections.Generic;
using System.Diagnostics;
using System.ComponentModel;
using System.Runtime.InteropServices;
public sealed class VivaMemoryProcess : IDisposable {
    [StructLayout(LayoutKind.Sequential)] struct Security { public int length; public IntPtr descriptor; public int inherit; }
    [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)] struct Startup { public int cb; public string reserved, desktop, title; public int x,y,width,height,charsX,charsY,fill,flags; public short show,reserved2; public IntPtr reservedPointer,input,output,error; }
    [StructLayout(LayoutKind.Sequential)] struct ProcessInfo { public IntPtr process,thread; public int processId,threadId; }
    [StructLayout(LayoutKind.Sequential,CharSet=CharSet.Unicode)] struct Entry { public uint size,usage,id;public UIntPtr heap;public uint module,threads,parent;public int priority;public uint flags;[MarshalAs(UnmanagedType.ByValTStr,SizeConst=260)] public string executable; }
    [DllImport("user32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern IntPtr CreateDesktop(string name, IntPtr device, IntPtr mode, int flags, uint access, IntPtr security);
    [DllImport("user32.dll")] static extern bool CloseDesktop(IntPtr desktop);
    [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern IntPtr CreateFile(string path,uint access,uint share,ref Security security,uint disposition,uint flags,IntPtr template);
    [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool CreateProcess(string app,StringBuilder command,IntPtr processAttributes,IntPtr threadAttributes,bool inherit,uint flags,IntPtr environment,string directory,ref Startup startup,out ProcessInfo process);
    [DllImport("kernel32.dll")] static extern bool GetExitCodeProcess(IntPtr handle,out uint code);
    [DllImport("kernel32.dll")] static extern bool TerminateProcess(IntPtr handle,uint code);
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    [DllImport("kernel32.dll")] static extern IntPtr CreateToolhelp32Snapshot(uint flags,uint process);
    [DllImport("kernel32.dll",CharSet=CharSet.Unicode)] static extern bool Process32First(IntPtr snapshot,ref Entry entry);
    [DllImport("kernel32.dll",CharSet=CharSet.Unicode)] static extern bool Process32Next(IntPtr snapshot,ref Entry entry);
    delegate bool EnumWindowsCallback(IntPtr window,IntPtr parameter);
    [DllImport("user32.dll")] static extern bool EnumDesktopWindows(IntPtr desktop,EnumWindowsCallback callback,IntPtr parameter);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr window,out int process);
    [DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr window,StringBuilder title,int count);
    [DllImport("user32.dll")] static extern bool PostMessage(IntPtr window,uint message,IntPtr w,IntPtr l);
    IntPtr desktop,output;
    ProcessInfo process;
    readonly DateTime launched=DateTime.UtcNow;
    readonly Dictionary<int,DateTime> owned=new Dictionary<int,DateTime>();
    readonly Dictionary<int,long> observedCPU=new Dictionary<int,long>();
    readonly Stopwatch clock=Stopwatch.StartNew();
    public VivaMemoryProcess(string executable,string directory,string log,string arguments) {
        string desktopName="VivaMemoryQA_"+Guid.NewGuid().ToString("N");
        desktop=CreateDesktop(desktopName,IntPtr.Zero,IntPtr.Zero,0,0x01FF,IntPtr.Zero);
        if(desktop==IntPtr.Zero)throw new Win32Exception();
        var security=new Security{length=Marshal.SizeOf(typeof(Security)),inherit=1};
        output=CreateFile(log,0x40000000,3,ref security,2,0x80,IntPtr.Zero);
        if(output==new IntPtr(-1))throw new Win32Exception();
        var startup=new Startup{cb=Marshal.SizeOf(typeof(Startup)),desktop=desktopName,flags=0x100,output=output,error=output};
        var command=new StringBuilder("\""+executable+"\" "+arguments);
        if(!CreateProcess(executable,command,IntPtr.Zero,IntPtr.Zero,true,0x08000000,IntPtr.Zero,directory,ref startup,out process))throw new Win32Exception();
        using(var root=Process.GetProcessById(process.processId)){owned[root.Id]=root.StartTime.ToUniversalTime();}
    }
    public void CloseWindow() {
        EnumWindowsCallback closeOwnWindow=(window,parameter)=>{
            int owner;GetWindowThreadProcessId(window,out owner);
            var title=new StringBuilder(256);GetWindowText(window,title,256);
            string caption=title.ToString();
            if(owner==process.processId&&(caption=="EditorX"||caption=="Viva Editor")) {
                PostMessage(window,0x10,IntPtr.Zero,IntPtr.Zero);
            }
            return true;
        };
        EnumDesktopWindows(desktop,closeOwnWindow,IntPtr.Zero);
    }
    public int RootId { get{return process.processId;} }
    public int ExitCode { get{uint code;GetExitCodeProcess(process.process,out code);return (int)code;} }
    public long Elapsed { get{return clock.ElapsedMilliseconds;} }
    public string Sample(string phase) {
        var parents=new Dictionary<int,int>();
        IntPtr snapshot=CreateToolhelp32Snapshot(2,0);
        if(snapshot!=new IntPtr(-1)){
            try{var entry=new Entry{size=(uint)Marshal.SizeOf(typeof(Entry))};if(Process32First(snapshot,ref entry)){do{parents[(int)entry.id]=(int)entry.parent;}while(Process32Next(snapshot,ref entry));}}
            finally{CloseHandle(snapshot);}
        }
        var liveOwned=new HashSet<int>();
        foreach(var pair in owned){try{using(var parent=Process.GetProcessById(pair.Key)){if(parent.StartTime.ToUniversalTime()==pair.Value&&!parent.HasExited)liveOwned.Add(pair.Key);}}catch{}}
        bool changed;
        do{changed=false;foreach(var pair in parents){if(owned.ContainsKey(pair.Key)||!liveOwned.Contains(pair.Value))continue;try{using(var child=Process.GetProcessById(pair.Key)){var birth=child.StartTime.ToUniversalTime();if(birth>=launched.AddSeconds(-1)){owned[pair.Key]=birth;liveOwned.Add(pair.Key);changed=true;}}}catch{}}}while(changed);
        long rootPrivate=0,rootWorking=0,childPrivate=0,childWorking=0,cpu=0;int childCount=0,rootHandles=0;var childIds=new List<int>();
        foreach(var pair in owned){try{using(var current=Process.GetProcessById(pair.Key)){if(current.StartTime.ToUniversalTime()!=pair.Value)continue;current.Refresh();if(current.HasExited)continue;long consumed=(long)current.TotalProcessorTime.TotalMilliseconds;cpu+=consumed;observedCPU[pair.Key]=consumed;if(pair.Key==process.processId){rootPrivate=current.PrivateMemorySize64;rootWorking=current.WorkingSet64;rootHandles=current.HandleCount;}else{childPrivate+=current.PrivateMemorySize64;childWorking+=current.WorkingSet64;childCount++;childIds.Add(pair.Key);}}}catch{}}
        long rootCPU=0,childrenCPU=0;foreach(var pair in observedCPU){if(pair.Key==process.processId)rootCPU=pair.Value;else childrenCPU+=pair.Value;}
        return "{\"ElapsedMS\":"+Elapsed+",\"Phase\":\""+phase+"\",\"RootPID\":"+RootId+",\"RootPrivateBytes\":"+rootPrivate+",\"RootWorkingSet\":"+rootWorking+",\"RootHandles\":"+rootHandles+",\"ChildPrivateBytes\":"+childPrivate+",\"ChildWorkingSet\":"+childWorking+",\"ChildCount\":"+childCount+",\"LiveCPUTimeMS\":"+cpu+",\"RootCPUTimeMS\":"+rootCPU+",\"ChildrenObservedCPUTimeMS\":"+childrenCPU+",\"ChildPIDs\":["+String.Join(",",childIds)+"]}";
    }
    public void Dispose(){
        if(process.process!=IntPtr.Zero&&ExitCode==259){TerminateProcess(process.process,124);}
        foreach(var pair in owned){if(pair.Key==process.processId)continue;try{using(var child=Process.GetProcessById(pair.Key)){if(child.StartTime.ToUniversalTime()==pair.Value&&!child.HasExited)child.Kill();}}catch{}}
        if(process.thread!=IntPtr.Zero)CloseHandle(process.thread);if(process.process!=IntPtr.Zero)CloseHandle(process.process);if(output!=IntPtr.Zero&&output!=new IntPtr(-1))CloseHandle(output);if(desktop!=IntPtr.Zero)CloseDesktop(desktop);
    }
}
"@
    $runner=[VivaMemoryProcess]::new($testExecutable,$SourceDirectory,$logPath,$arguments)
    $samplePath=Join-Path $outputDirectory 'process-memory.jsonl'
    $writer=[IO.StreamWriter]::new($samplePath,$false,[Text.UTF8Encoding]::new($false))
    try {
        $phase='starting';$exitObserved=$null;$closeRequested=$false
        while($runner.Elapsed -lt ($TimeoutSeconds*1000)) {
            if ($ExecutablePath -and !$closeRequested) {
                $phase = 'release-running'
                if ($runner.Elapsed -ge ($ObserveSeconds*1000)) {
                    $runner.CloseWindow();$closeRequested=$true;$phase='release-closing'
                }
            }
            $phaseFile=Join-Path $outputDirectory 'phase.json'
            if(Test-Path -LiteralPath $phaseFile){try{$state=Get-Content -LiteralPath $phaseFile -Raw | ConvertFrom-Json;$phase=$state.Phase+'-'+$state.Cycle}catch{}}
            if($runner.ExitCode -ne 259){if($null -eq $exitObserved){$exitObserved=$runner.Elapsed};$phase='process-exited'}
            $writer.WriteLine($runner.Sample($phase));$writer.Flush()
            if($null -ne $exitObserved -and $runner.Elapsed-$exitObserved -ge 5000){break}
            Start-Sleep -Milliseconds 500
        }
        $exitCode=$runner.ExitCode
    } finally {$writer.Dispose();$runner.Dispose()}
    Get-Content -LiteralPath $logPath -Tail 15
    $samples=Get-Content -LiteralPath $samplePath | ForEach-Object {$_ | ConvertFrom-Json}
    $summary=$samples | Group-Object Phase | ForEach-Object {
        $last=$_.Group[-1]
        [pscustomobject]@{Phase=$_.Name;RootPrivateMiB=[math]::Round($last.RootPrivateBytes/1MB,2);ChildrenPrivateMiB=[math]::Round($last.ChildPrivateBytes/1MB,2);TotalPrivateMiB=[math]::Round(($last.RootPrivateBytes+$last.ChildPrivateBytes)/1MB,2);RootWorkingMiB=[math]::Round($last.RootWorkingSet/1MB,2);ChildrenWorkingMiB=[math]::Round($last.ChildWorkingSet/1MB,2);ChildCount=$last.ChildCount;RootHandles=$last.RootHandles}
    }
    $summary | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $outputDirectory 'summary.json') -Encoding utf8
    $summary | Format-Table -AutoSize
    [pscustomobject]@{OutputDirectory=$outputDirectory;ExitCode=$exitCode} | ConvertTo-Json
    if($exitCode -ne 0){throw "Native memory test failed: $exitCode"}
    if($samples[-1].ChildCount -ne 0){throw 'Owned child processes survived five seconds after application exit'}
} finally {
    Remove-Item Env:VIVA_MEMORY_OUTPUT -ErrorAction SilentlyContinue
    Remove-Item Env:VIVA_MEMORY_CYCLES -ErrorAction SilentlyContinue
    Remove-Item Env:VIVA_EDITOR_DATA_DIR -ErrorAction SilentlyContinue
    Remove-Item Env:WEBVIEW2_USER_DATA_FOLDER -ErrorAction SilentlyContinue
    Pop-Location
}
