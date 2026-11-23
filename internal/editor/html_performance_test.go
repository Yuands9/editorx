package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func BenchmarkHTMLPreparation(b *testing.B) {
	source := "<!doctype html><html><head><script>const text='<head>not a tag</head>';</script><title>中文</title></head><body>" + strings.Repeat(`<div class="item"><span>中文内容</span><img src="image.png"></div>`, 20000) + "</body></html>"
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		previewHTML(source, `D:\网页\index.html`)
	}
}

func TestNativeHTMLStartupTiming(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native timing is opt-in")
	}
	directory := t.TempDir()
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(directory, "WebView2"))
	paths := make([]string, 4)
	for i := range paths {
		paths[i] = filepath.Join(directory, fmt.Sprintf("页面%d.html", i))
		content := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body><h1>中文页面</h1><script>document.body.dataset.ready='yes';</script>" + strings.Repeat("<p>预览文本</p>\n", 1000) + "</body></html>"
		if err := os.WriteFile(paths[i], []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newEditorApp()
	m.dataDirectory = filepath.Join(directory, "data")
	var started, sourceReady, previewWrittenAt time.Time
	var failure error
	phase, sample := 0, 0
	completed := false
	stop := make(chan struct{})
	defer close(stop)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		deadline := time.Now().Add(40 * time.Second)
		finish := func(err error) {
			failure, completed = err, true
			for _, tab := range m.tabs {
				tab.html.close()
			}
			application.Quit()
		}
		go func() {
			ticker := time.NewTicker(2 * time.Millisecond)
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
							finish(fmt.Errorf("timing timeout phase %d", phase))
							return
						}
						if m.active != nil && m.active.html.errorText != "" {
							finish(fmt.Errorf("preview: %s", m.active.html.errorText))
							return
						}
						switch phase {
						case 0:
							if !m.webReady {
								return
							}
							phase = 1
							started = time.Now()
							m.load(paths[sample])
							sourceReady = time.Now()
						case 1:
							if !m.active.html.ready {
								return
							}
							t.Logf("open[%d]=%.3f ms (file and tab %.3f ms)", sample, float64(time.Since(started).Microseconds())/1000, float64(sourceReady.Sub(started).Microseconds())/1000)
							info, err := os.Stat(m.active.html.file)
							if err != nil {
								finish(err)
								return
							}
							previewWrittenAt = info.ModTime()
							phase = 2
							started = time.Now()
							m.refreshHTML(m.active)
						case 2:
							if !m.active.html.ready {
								return
							}
							t.Logf("refresh[%d]=%.3f ms", sample, float64(time.Since(started).Microseconds())/1000)
							info, err := os.Stat(m.active.html.file)
							if err != nil || !info.ModTime().Equal(previewWrittenAt) {
								finish(fmt.Errorf("unchanged refresh rewrote the preview: %v", err))
								return
							}
							tab := m.active
							view := tab.html.view
							m.activate(nil)
							m.activate(tab)
							if tab.html.view != view || !tab.html.ready {
								finish(fmt.Errorf("returning to the HTML tab rebuilt its preview"))
								return
							}
							sample++
							if sample == len(paths) {
								finish(nil)
								return
							}
							phase = 0
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
	if !completed {
		t.Fatal("timing ended early")
	}
}
