package editor

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

type memoryObservation struct {
	Phase          string
	Cycle          int
	ElapsedMS      int64
	HeapBytes      uint64
	HeapObjects    uint64
	HeapInuse      uint64
	HeapSys        uint64
	TotalAllocated uint64
	GCCount        uint32
	Goroutines     int
	Tabs           int
	LoadMS         int64
	CloseMS        int64
}

// This opt-in test runs only on the harness's isolated Windows desktop. The external
// harness records root + proven descendant private bytes, not unrelated WebView2 apps.
func TestNativeMemoryStress(t *testing.T) {
	output := os.Getenv("VIVA_MEMORY_OUTPUT")
	if output == "" {
		t.Skip("native memory stress is opt-in")
	}
	cycles := 6
	if value, err := strconv.Atoi(os.Getenv("VIVA_MEMORY_CYCLES")); err == nil && value > 0 {
		cycles = value
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(output, "documents")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(output, "WebView2"))
	textPath, jsonPath, imagePath := filepath.Join(directory, "large.txt"), filepath.Join(directory, "large.json"), filepath.Join(directory, "large.png")
	htmlPath, sheetPath, wordPath := filepath.Join(directory, "page.html"), filepath.Join(directory, "book.xlsx"), filepath.Join(directory, "document.docx")
	pdfPath := filepath.Join(directory, "large.pdf")
	write := func(path string, data []byte) {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(textPath, []byte(strings.Repeat("memory-check 中文文本配置 enabled=true\n", 20000)))
	write(jsonPath, []byte("["+strings.Repeat(`{"name":"中文配置","enabled":true,"port":8080},`, 4999)+`{"name":"中文配置","enabled":true,"port":8080}]`))
	picture := image.NewNRGBA(image.Rect(0, 0, 2048, 1536))
	for y := 0; y < 1536; y++ {
		for x := 0; x < 2048; x++ {
			picture.SetNRGBA(x, y, color.NRGBA{uint8(x / 8), uint8(y / 6), uint8((x + y) / 12), 255})
		}
	}
	imageFile, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(imageFile, picture); err != nil {
		t.Fatal(err)
	}
	imageFile.Close()
	picture = nil
	write(htmlPath, []byte("<!doctype html><meta charset=utf-8><h1>内存测试</h1>"+strings.Repeat("<p>本地 HTML 中文预览</p>", 2000)))
	write(sheetPath, sampleWorkbook(t))
	write(wordPath, sampleDocx(t))
	write(pdfPath, makeLargePDF(largeOpenPictures(t)))
	observations, err := os.Create(filepath.Join(output, "go-memory.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer observations.Close()
	started := time.Now()
	m := newEditorApp()
	store, err := openSessionStore(filepath.Join(output, "data"))
	if err != nil {
		t.Fatal(err)
	}
	store.attach(m)
	record := func(phase string, cycle int, load, close time.Duration) {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		tabs := 0
		if m != nil {
			tabs = len(m.tabs)
		}
		value := memoryObservation{Phase: phase, Cycle: cycle, ElapsedMS: time.Since(started).Milliseconds(), HeapBytes: stats.HeapAlloc, HeapObjects: stats.HeapObjects, HeapInuse: stats.HeapInuse, HeapSys: stats.HeapSys, TotalAllocated: stats.TotalAlloc, GCCount: stats.NumGC, Goroutines: runtime.NumGoroutine(), Tabs: tabs, LoadMS: load.Milliseconds(), CloseMS: close.Milliseconds()}
		data, _ := json.Marshal(value)
		observations.Write(append(data, '\n'))
		observations.Sync()
		_ = writeFile(filepath.Join(output, "phase.json"), data)
	}
	record("starting", 0, 0, 0)
	stop := make(chan struct{})
	completed := false
	var failure error
	err = app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		cycle, phase := 0, 0
		next := time.Now().Add(1500 * time.Millisecond)
		deadline := time.Now().Add(180 * time.Second)
		var loaded []*editorTab
		var loadStart, closeStart time.Time
		var loadDuration time.Duration
		finish := func(err error) {
			if completed {
				return
			}
			completed = true
			failure = err
			for _, tab := range m.tabs {
				tab.html.close()
				tab.office.close()
				tab.preview.close()
			}
			application.Quit()
		}
		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if completed {
							return
						}
						if time.Now().After(deadline) {
							finish(fmt.Errorf("memory cycle timeout: cycle=%d phase=%d", cycle, phase))
							return
						}
						if m.showError {
							finish(fmt.Errorf("memory cycle error: %s", m.errorText))
							return
						}
						switch phase {
						case 0:
							if time.Now().Before(next) {
								return
							}
							runtime.GC()
							record("idle", 0, 0, 0)
							phase = 7
							next = time.Now().Add(1500 * time.Millisecond)
						case 1:
							cycle++
							record("loading", cycle, 0, 0)
							loadStart = time.Now()
							loaded = nil
							for _, path := range []string{textPath, jsonPath, imagePath, htmlPath, sheetPath, wordPath, pdfPath} {
								m.load(path)
								tab := m.active
								loaded = append(loaded, tab)
								if tab.format() == "HTML" {
									m.refreshHTML(tab)
								}
								if tab.office != nil {
									m.openOffice(tab)
								}
							}
							m.invalidator.Invalidate()
							phase = 2
						case 2:
							for _, tab := range loaded {
								if tab.office != nil {
									if tab.office.errorText != "" {
										finish(fmt.Errorf("office: %s", tab.office.errorText))
										return
									}
									if !tab.office.ready {
										return
									}
								}
								if tab.preview != nil && tab.preview.kind == "PDF" {
									if tab.preview.issue != "" {
										finish(fmt.Errorf("PDF: %s", tab.preview.issue))
										return
									}
									if !tab.preview.ready {
										return
									}
								}
								if tab.format() == "HTML" && !tab.html.ready {
									return
								}
							}
							loadDuration = time.Since(loadStart)
							if err := store.save(); err != nil {
								finish(err)
								return
							}
							record("loaded", cycle, loadDuration, 0)
							next = time.Now().Add(1500 * time.Millisecond)
							phase = 3
						case 3:
							if time.Now().Before(next) {
								return
							}
							closeStart = time.Now()
							for _, tab := range loaded {
								m.closeTab(tab)
							}
							loaded = nil
							m.invalidator.Invalidate()
							phase = 4
						case 4:
							if m.confirm {
								finish(fmt.Errorf("unchanged document unexpectedly dirty"))
								return
							}
							if len(m.tabs) != 0 || m.active != nil {
								return
							}
							if err := store.save(); err != nil {
								finish(err)
								return
							}
							record("closed", cycle, loadDuration, time.Since(closeStart))
							next = time.Now().Add(1500 * time.Millisecond)
							phase = 5
						case 5:
							if time.Now().Before(next) {
								return
							}
							runtime.GC()
							runtime.GC()
							record("retained", cycle, loadDuration, time.Since(closeStart))
							if cycle < cycles {
								phase = 7
								next = time.Now().Add(1200 * time.Millisecond)
								return
							}
							if file, err := os.Create(filepath.Join(output, "retained-heap.pprof")); err == nil {
								pprof.WriteHeapProfile(file)
								file.Close()
							}
							phase = 6
							next = time.Now().Add(3500 * time.Millisecond)
						case 7:
							if time.Now().Before(next) {
								return
							}
							phase = 1
						case 6:
							if time.Now().Before(next) {
								return
							}
							record("settled", cycle, 0, 0)
							finish(nil)
						}
					})
				}
			}
		}()
		return nil
	})
	close(stop)
	m = nil
	store = nil
	runtime.GC()
	runtime.GC()
	record("window-closed", cycles, 0, 0)
	time.Sleep(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if failure != nil {
		t.Fatal(failure)
	}
	if !completed {
		t.Fatal("memory run exited early")
	}
	t.Logf("native memory observations: %s", output)
}
