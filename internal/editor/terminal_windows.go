package editor

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

const windowsPseudoConsoleAttribute = 0x00020016
const windowsExtendedStartupInfo = 0x00080000

var (
	shellKernel                  = syscall.NewLazyDLL("kernel32.dll")
	createPseudoConsole          = shellKernel.NewProc("CreatePseudoConsole")
	resizePseudoConsole          = shellKernel.NewProc("ResizePseudoConsole")
	closePseudoConsole           = shellKernel.NewProc("ClosePseudoConsole")
	initializeTerminalAttributes = shellKernel.NewProc("InitializeProcThreadAttributeList")
	updateTerminalAttribute      = shellKernel.NewProc("UpdateProcThreadAttribute")
	deleteTerminalAttributes     = shellKernel.NewProc("DeleteProcThreadAttributeList")
)

type terminalSize struct{ cols, rows int }

func (size terminalSize) word() uintptr {
	return uintptr(min(512, max(2, size.cols)) | min(256, max(2, size.rows))<<16)
}

type terminalInput struct {
	text string
	size terminalSize
}

type terminalProcess struct {
	in    *os.File
	out   *os.File
	con   uintptr
	proc  syscall.Handle
	input chan terminalInput
	stop  chan struct{}
	done  chan struct{}
}

func startTerminalProcess(directory string, size terminalSize) (*terminalProcess, error) {
	if err := createPseudoConsole.Find(); err != nil {
		return nil, fmt.Errorf("本地终端需要 Windows 10 1809 或更新版本：%w", err)
	}
	inReader, inWriter, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer inReader.Close()
	outReader, outWriter, err := os.Pipe()
	if err != nil {
		inWriter.Close()
		return nil, err
	}
	defer outWriter.Close()
	var console uintptr
	res, _, _ := createPseudoConsole.Call(size.word(), inReader.Fd(), outWriter.Fd(), 0, uintptr(unsafe.Pointer(&console)))
	runtime.KeepAlive(inReader)
	runtime.KeepAlive(outWriter)
	if int32(res) < 0 {
		inWriter.Close()
		outReader.Close()
		return nil, fmt.Errorf("创建本地终端失败（0x%08X）", uint32(res))
	}

	exe := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	process, err := startInPseudoConsole(exe, directory, console)
	if err != nil {
		inWriter.Close()
		outReader.Close()
		closePseudoConsole.Call(console)
		return nil, err
	}
	return &terminalProcess{
		in: inWriter, out: outReader, con: console, proc: process,
		input: make(chan terminalInput, 128), stop: make(chan struct{}, 1), done: make(chan struct{}),
	}, nil
}

func startInPseudoConsole(exe, directory string, console uintptr) (syscall.Handle, error) {
	application, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		return 0, err
	}
	command, err := syscall.UTF16PtrFromString(syscall.EscapeArg(exe) + " -NoLogo -NoProfile")
	if err != nil {
		return 0, err
	}
	working, err := syscall.UTF16PtrFromString(directory)
	if err != nil {
		return 0, err
	}
	var bytesNeeded uintptr
	initializeTerminalAttributes.Call(0, 1, 0, uintptr(unsafe.Pointer(&bytesNeeded)))
	if bytesNeeded == 0 {
		return 0, fmt.Errorf("无法为 PowerShell 准备终端属性")
	}
	storage := make([]byte, bytesNeeded)
	attributes := unsafe.Pointer(&storage[0])
	ok, _, lastErr := initializeTerminalAttributes.Call(uintptr(attributes), 1, 0, uintptr(unsafe.Pointer(&bytesNeeded)))
	if ok == 0 {
		return 0, lastErr
	}
	defer func() {
		deleteTerminalAttributes.Call(uintptr(attributes))
		runtime.KeepAlive(storage)
	}()
	ok, _, lastErr = updateTerminalAttribute.Call(uintptr(attributes), 0, windowsPseudoConsoleAttribute,
		console, unsafe.Sizeof(console), 0, 0)
	if ok == 0 {
		return 0, lastErr
	}
	startup := struct {
		syscall.StartupInfo
		attributes unsafe.Pointer
	}{attributes: attributes}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	// Zero standard handles allow ConPTY to supply its console handles even under a redirected parent.
	startup.Flags = syscall.STARTF_USESTDHANDLES
	var processInfo syscall.ProcessInformation
	err = syscall.CreateProcess(application, command, nil, nil, false, windowsExtendedStartupInfo,
		nil, working, &startup.StartupInfo, &processInfo)
	if err != nil {
		return 0, fmt.Errorf("启动 PowerShell 失败：%w", err)
	}
	syscall.CloseHandle(processInfo.Thread)
	return processInfo.Process, nil
}

func (process *terminalProcess) run(output func([]byte), finished func()) {
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 8192)
		for {
			n, err := process.out.Read(buffer)
			if n > 0 {
				output(buffer[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go process.writeInput()
	go func() {
		exited := make(chan struct{})
		go func() {
			_, _ = syscall.WaitForSingleObject(process.proc, syscall.INFINITE)
			close(exited)
		}()
		select {
		case <-process.stop:
			_ = syscall.TerminateProcess(process.proc, 1)
			<-exited
		case <-exited:
		}
		close(process.done)
		closePseudoConsole.Call(process.con)
		_ = process.in.Close()
		<-readDone
		_ = process.out.Close()
		_ = syscall.CloseHandle(process.proc)
		finished()
	}()
}

func (process *terminalProcess) writeInput() {
	for {
		select {
		case <-process.done:
			return
		case message := <-process.input:
			if message.size.cols > 0 && message.size.rows > 0 {
				resizePseudoConsole.Call(process.con, message.size.word())
				continue
			}
			if _, err := io.WriteString(process.in, message.text); err != nil {
				return
			}
		}
	}
}

func (process *terminalProcess) enqueue(message terminalInput) bool {
	select {
	case <-process.done:
		return false
	default:
	}
	select {
	case process.input <- message:
		return true
	default:
		return false
	}
}

func (process *terminalProcess) close() {
	select {
	case process.stop <- struct{}{}:
	default:
	}
}
