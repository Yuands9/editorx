package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// The runner gives each case a new process and WebView profile. A real Win32
// click opens the file through the project's tree, on the isolated desktop.
func TestNativeLargeOpen(t *testing.T) {
	format, directory, profile := os.Getenv("VIVA_LARGE_FORMAT"), os.Getenv("VIVA_LARGE_FIXTURES"), os.Getenv("VIVA_LARGE_PROFILE")
	if format == "" || directory == "" || profile == "" {
		t.Skip("run tools/large-open.ps1 to measure a cold open")
	}
	switch format {
	case "xlsx", "docx", "pdf", "json", "html":
	default:
		t.Fatalf("unknown file fixture %q", format)
	}
	name := "large." + format
	path := filepath.Join(directory, name)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", profile)
	t.Setenv("VIVA_SCALE", "1")

	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addProjects(directory)
	if m.showError {
		t.Fatal(m.errorText)
	}

	var elapsed time.Duration
	var failure error
	var finished bool
	stop := make(chan struct{})
	defer close(stop)
	err = app.RunApplication(func(application *app.Application) error {
		page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
		page.SetApplication(application)
		config := ui.Configure(page, m.editorWindowConfig(application))

		var row gfx.Rect
		paint := config.Paint
		config.Paint = func(canvas *gfx.Canvas, size gfx.Size) {
			paint(canvas, size)
			if row.Width > 0 {
				return
			}
			for _, hit := range canvas.Hits() {
				if hit.Label == name && !hit.Disabled && hit.Bounds.Width > 0 && hit.Bounds.Height > 0 {
					row = hit.Bounds
					break
				}
			}
		}
		if _, err := application.OpenWindow(config, new(gfx.Canvas)); err != nil {
			page.Close()
			return err
		}

		hwnd, _, _ := editorUser.NewProc("GetActiveWindow").Call()
		var owner uint32
		editorUser.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if hwnd == 0 || owner != uint32(os.Getpid()) {
			return fmt.Errorf("the active test window is not owned by this process")
		}
		post := editorUser.NewProc("PostMessageW")
		deadline := time.Now().Add(65 * time.Second)
		var started time.Time
		var editSent bool
		var originalRunes int
		finish := func(problem error) {
			failure, finished = problem, true
			m.releaseWindowState()
			application.Quit()
		}
		go func() {
			ticker := time.NewTicker(8 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if finished {
							return
						}
						if m.showError {
							finish(fmt.Errorf("opening %s: %s", name, m.errorText))
							return
						}
						if time.Now().After(deadline) {
							finish(fmt.Errorf("timeout for %s: clicked=%t, edit attempted=%t, status=%s", name, !started.IsZero(), editSent, m.status))
							return
						}
						if started.IsZero() {
							if !m.webReady || row.Width == 0 {
								return
							}
							x, y := uint16(row.X+row.Width/2), uint16(row.Y+row.Height/2)
							point := uintptr(uint32(x) | uint32(y)<<16)
							started = time.Now()
							down, _, _ := post.Call(hwnd, 0x0201, 1, point)
							up, _, _ := post.Call(hwnd, 0x0202, 0, point)
							if down == 0 || up == 0 {
								finish(fmt.Errorf("could not dispatch the tree click for %s", name))
							}
							return
						}

						tab := m.active
						if tab == nil || tab.doc.path != path {
							return
						}
						ready := false
						switch format {
						case "pdf":
							if tab.preview.issue != "" {
								finish(fmt.Errorf("opening PDF: %s", tab.preview.issue))
								return
							}
							ready = tab.preview.ready
						case "xlsx", "docx":
							if tab.office.errorText != "" {
								finish(fmt.Errorf("opening Office editor: %s", tab.office.errorText))
								return
							}
							if tab.office.ready && !editSent {
								editSent = true
								edit := `window.office.workbook.getActiveSheet().getRange("A1").setValue("Viva benchmark edit")`
								if format == "docx" {
									edit = `window.office.editor.activeEditor.doc.insert({type:"text", value:"验收输入"})`
								}
								tab.office.view.Eval(`try { ` + edit + ` } catch (error) { window.viva.postMessage(JSON.stringify({kind:"error", message:error.message})) }`)
							}
							ready = editSent && tab.modified()
						default:
							if !editSent && !tab.source.focus {
								editSent = true
								originalRunes = tab.source.text.RuneCount()
								written, _, _ := post.Call(hwnd, 0x0102, ' ', 1)
								if written == 0 {
									finish(fmt.Errorf("could not dispatch the first text input"))
									return
								}
							}
							ready = editSent && tab.source.text.RuneCount() == originalRunes+1
						}
						if ready {
							elapsed = time.Since(started)
							finish(nil)
						}
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
	if !finished || elapsed <= 0 {
		t.Fatal("native opening test ended early")
	}
	milestone := "editable"
	if format == "pdf" {
		milestone = "preview"
	}
	t.Logf("VIVA_OPEN %s %d %d %s", format, info.Size(), elapsed.Milliseconds(), milestone)
}
