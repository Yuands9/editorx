//go:build windows

package editor

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func TestNativeLargeEdit(t *testing.T) {
	fixture := os.Getenv("VIVA_LARGE_EDIT_FIXTURE")
	if fixture == "" {
		t.Skip("测量时请设置 VIVA_LARGE_EDIT_FIXTURE 指向日志样例")
	}
	file, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "catalina-copy.out")
	err = writeFileFrom(path, file, info.Size())
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load(path)
	tab := m.active
	if m.showError || tab == nil || tab.largeText == nil {
		t.Fatalf("打开测试副本失败：%s", m.errorText)
	}
	// Read process private bytes on the Windows desktop, independent of the
	// Go heap counters and the system's file cache.
	type memoryCounters struct {
		Size, Faults                                                                                          uint32
		PeakWorkingSet, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile, Private uintptr
	}
	memoryInfo := syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
	private := func() uintptr {
		v := memoryCounters{Size: uint32(unsafe.Sizeof(memoryCounters{}))}
		ok, _, _ := memoryInfo.Call(^uintptr(0), uintptr(unsafe.Pointer(&v)), uintptr(v.Size))
		if ok == 0 {
			return 0
		}
		return v.Private
	}
	var failure error
	var opened, touched, searched, saved time.Duration
	var baseline, editing, peak, closed uintptr
	var draftBytes, matches int
	var rawPosition int64
	var beforeText string
	var done bool
	stop := make(chan struct{})
	defer close(stop)
	err = app.RunApplication(func(application *app.Application) error {
		page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme, Styles: &editorControlStyles})
		page.SetApplication(application)
		config := ui.Configure(page, m.editorWindowConfig(application))
		paint := config.Paint
		start := time.Now()
		var firstPaint time.Time
		phase := 0
		config.Paint = func(canvas *gfx.Canvas, size gfx.Size) {
			paint(canvas, size)
			if firstPaint.IsZero() && tab.largeText.editing != nil && tab.largeText.editing.bounds.Height > 0 {
				firstPaint = time.Now()
				opened = firstPaint.Sub(start)
			}
		}
		window, err := application.OpenWindow(config, new(gfx.Canvas))
		if err != nil {
			page.Close()
			return err
		}
		m.nativeWindow = window
		hwnd, _, _ := editorUser.NewProc("GetActiveWindow").Call()
		var process uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&process)))
		if hwnd == 0 || process != uint32(os.Getpid()) {
			return fmt.Errorf("测试窗口句柄无效")
		}
		post := editorUser.NewProc("PostMessageW")
		deadline := time.Now().Add(55 * time.Second)
		finish := func(err error) {
			failure, done = err, true
			m.releaseWindowState()
			application.Quit()
		}
		go func() {
			tick := time.NewTicker(12 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					application.Post(func() {
						if done {
							return
						}
						if m.showError {
							finish(fmt.Errorf("%s", m.errorText))
							return
						}
						if time.Now().After(deadline) {
							finish(fmt.Errorf("阶段 %d 超时：%s", phase, tab.largeText.problem))
							return
						}
						if phase >= 1 && phase <= 6 {
							peak = max(peak, private())
						}
						switch phase {
						case 0:
							if tab.largeText.lines.file == nil {
								return
							}
							baseline = private()
							start = time.Now()
							m.startLargeTextEdit(tab, tab.doc.encoding)
						case 1:
							if firstPaint.IsZero() {
								return
							}
							if time.Since(firstPaint) < 2*time.Second {
								return
							}
							editing = private()
							line := max(1, tab.largeText.editing.revision.breaks-100)
							if err := m.revealTextLine(tab, line); err != nil {
								finish(err)
								return
							}
							edit := tab.largeText.editing
							rawPosition = edit.position.caret + int64(len(tab.doc.encoding.bomPrefix()))
							if !tab.doc.bom {
								rawPosition = edit.position.caret
							}
							var err error
							beforeText, err = edit.revision.read(edit.position.caret, min(edit.revision.size, edit.position.caret+12))
							if err != nil {
								finish(err)
								return
							}
						case 2:
							if !tab.source.focused {
								return
							}
							start = time.Now()
							if ok, _, _ := post.Call(hwnd, 0x0102, uintptr('编'), 0); ok == 0 {
								finish(fmt.Errorf("无法发送 Windows 文本输入消息"))
								return
							}
						case 3:
							if !tab.modified() {
								return
							}
							touched = time.Since(start)
							edit := tab.largeText.editing
							if mark, err := edit.revision.read(edit.position.caret-3, edit.position.caret); err != nil || mark != "编" {
								finish(fmt.Errorf("原生编辑没有写入选定位置"))
								return
							}
							tab.undoLargeEdit(false)
							if tab.modified() {
								finish(fmt.Errorf("撤销未恢复初始版本"))
								return
							}
							tab.undoLargeEdit(true)
							tab.search.field.SetText("Cache-Control: no-cache")
							start = time.Now()
							m.findAll(tab, false)
						case 4:
							if m.searchBatch != nil {
								return
							}
							searched = time.Since(start)
							matches = m.results.total
							if matches == 0 {
								finish(fmt.Errorf("全文搜索未找到测试词"))
								return
							}
							session, err := openSessionStore(filepath.Join(m.dataDirectory, "probe"))
							if err != nil {
								finish(err)
								return
							}
							session.attach(m)
							if err := session.save(); err != nil {
								finish(err)
								return
							}
							entry, err := os.Stat(filepath.Join(session.directory, "session.json"))
							if err != nil {
								finish(err)
								return
							}
							draftBytes = int(entry.Size())
							start = time.Now()
							m.saveTab(tab, false)
						case 5:
							if tab.largeText.saving != nil {
								return
							}
							saved = time.Since(start)
							file, err := os.Open(path)
							if err != nil {
								finish(err)
								return
							}
							got := make([]byte, len(beforeText)+len("编"))
							_, err = file.ReadAt(got, rawPosition)
							hash := sha256.New()
							if err == nil {
								_, err = io.Copy(hash, file)
							}
							after, statErr := file.Stat()
							file.Close()
							if err != nil || statErr != nil || !bytes.Equal(got, []byte("编"+beforeText)) ||
								tab.modified() || !bytes.Equal(hash.Sum(nil), tab.doc.diskHash[:]) || after.Size() != info.Size()+int64(len("编")) {
								finish(fmt.Errorf("流式保存输出验证失败"))
								return
							}
							if filepath.Clean(path) != filepath.Clean(tab.doc.path) {
								finish(fmt.Errorf("保存路径错误"))
								return
							}
							m.removeTab(tab)
							start = time.Now()
						case 6:
							if time.Since(start) < 3*time.Second {
								return
							}
							closed = private()
							finish(nil)
						}
						phase++
						m.invalidator.Invalidate()
					})
				}
			}
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if failure != nil {
		t.Fatal(failure)
	}
	if !done || baseline == 0 || editing == 0 || closed == 0 {
		t.Fatal("原生大文件测量提前退出")
	}
	t.Logf("LARGE_EDIT size=%d open_ms=%.1f input_ms=%.1f find_ms=%.1f save_ms=%.1f hits=%d private_before=%.1f private_edit=%.1f private_peak=%.1f private_closed=%.1f session_bytes=%d",
		info.Size(), float64(opened.Microseconds())/1000, float64(touched.Microseconds())/1000,
		float64(searched.Microseconds())/1000, float64(saved.Microseconds())/1000, matches,
		float64(baseline)/(1<<20), float64(editing)/(1<<20), float64(peak)/(1<<20), float64(closed)/(1<<20), draftBytes)
}
