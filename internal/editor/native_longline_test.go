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

func TestNativeLongHTMLText(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native long line rendering is opt-in")
	}
	directory := t.TempDir()
	m := newEditorApp()
	m.dataDirectory = directory
	source := "<!doctype html><html><head><meta charset=\"utf-8\"></head><body>" + strings.Repeat("<p>预览文本\t中文😀</p>", 1000) + "<p>查找末尾</p></body></html>"
	tab := m.addTab(document{path: filepath.Join(directory, "long.html"), saved: source, eol: "LF"}, "long.html")
	tab.htmlMode = "编辑"
	completed := false
	var failure error
	stop := make(chan struct{})
	defer close(stop)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		phase := 0
		go func() {
			ticker := time.NewTicker(180 * time.Millisecond)
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
						switch phase {
						case 0:
							tab.source.text.MoveTo(tab.source.text.RuneCount(), false)
							tab.source.focus = true
						case 1:
							if tab.source.view.Code.ScrollX() <= 0 {
								failure = fmt.Errorf("long line did not scroll horizontally")
							}
							tab.openSearch()
							tab.search.query.Insert("查找末尾")
							tab.search.refresh()
						case 2:
							if len(tab.search.matches) != 1 {
								failure = fmt.Errorf("long line search did not find final text")
							}
							tab.search.close()
							tab.source.text.MoveTo(0, false)
							tab.source.focus = true
						case 3:
							if tab.source.text.String() != source || tab.source.view.Snapshot.Wrap {
								failure = fmt.Errorf("long line source or wrapping changed")
							}
							completed = true
							application.Quit()
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
	if !completed {
		t.Fatal("native long-line window closed before completing")
	}
	t.Log("长行 HTML 原生绘制、中文/emoji/tab、水平滚动、末尾搜索和原文保留通过")
}
