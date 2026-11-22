using System;
using System.Text;
using System.ComponentModel;
using System.Runtime.InteropServices;
public static class VivaIsolatedDesktop {
    [StructLayout(LayoutKind.Sequential)] struct Security { public int length; public IntPtr descriptor; public int inherit; }
    [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)] struct Startup { public int cb; public string reserved, desktop, title; public int x,y,width,height,charsX,charsY,fill,flags; public short show,reserved2; public IntPtr reservedPointer,input,output,error; }
    [StructLayout(LayoutKind.Sequential)] struct ProcessInfo { public IntPtr process,thread; public int processId,threadId; }
    [DllImport("user32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern IntPtr CreateDesktop(string name, IntPtr device, IntPtr mode, int flags, uint access, IntPtr security);
    [DllImport("user32.dll",SetLastError=true)] static extern bool CloseDesktop(IntPtr desktop);
    [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern IntPtr CreateFile(string path,uint access,uint share,ref Security security,uint disposition,uint flags,IntPtr template);
    [DllImport("kernel32.dll",CharSet=CharSet.Unicode,SetLastError=true)] static extern bool CreateProcess(string app,StringBuilder command,IntPtr processAttributes,IntPtr threadAttributes,bool inherit,uint flags,IntPtr environment,string directory,ref Startup startup,out ProcessInfo process);
    [DllImport("kernel32.dll")] static extern uint WaitForSingleObject(IntPtr handle,uint milliseconds);
    [DllImport("kernel32.dll")] static extern bool GetExitCodeProcess(IntPtr handle,out uint code);
    [DllImport("kernel32.dll")] static extern bool TerminateProcess(IntPtr handle,uint code);
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    public static int Run(string executable,string directory,string log,int seconds,string testName) {
        string desktopName="VivaEditorQA_"+Guid.NewGuid().ToString("N");
        IntPtr desktop=CreateDesktop(desktopName,IntPtr.Zero,IntPtr.Zero,0,0x01FF,IntPtr.Zero);
        if(desktop==IntPtr.Zero) throw new Win32Exception();
        var security=new Security {length=Marshal.SizeOf(typeof(Security)),inherit=1};
        IntPtr output=CreateFile(log,0x40000000,3,ref security,2,0x80,IntPtr.Zero);
        if(output==new IntPtr(-1)) {CloseDesktop(desktop);throw new Win32Exception();}
        ProcessInfo process=new ProcessInfo();
        try {
            var startup=new Startup {cb=Marshal.SizeOf(typeof(Startup)),desktop=desktopName,flags=0x100,output=output,error=output};
            var command=new StringBuilder("\""+executable+"\" -test.run ^"+testName+"$ -test.v -test.timeout "+(seconds-5)+"s");
            if(!CreateProcess(executable,command,IntPtr.Zero,IntPtr.Zero,true,0x08000000,IntPtr.Zero,directory,ref startup,out process)) throw new Win32Exception();
            if(WaitForSingleObject(process.process,(uint)(seconds*1000))!=0) { TerminateProcess(process.process,124); WaitForSingleObject(process.process,3000); return 124; }
            uint code; GetExitCodeProcess(process.process,out code); return (int)code;
        } finally {
            if(process.thread!=IntPtr.Zero) CloseHandle(process.thread);
            if(process.process!=IntPtr.Zero) CloseHandle(process.process);
            CloseHandle(output); CloseDesktop(desktop);
        }
    }
}
