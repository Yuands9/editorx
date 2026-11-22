package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

// 由 tools/native-smoke.ps1 在独立、不可见的 Windows 桌面启动，不操作用户桌面。
func TestNativeHTMLPreview(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native WebView2 check is opt-in")
	}
	directory := t.TempDir()
	dataDirectory := filepath.Join(directory, "data")
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(dataDirectory, "WebView2"))
	fixtures := map[string][]byte{
		"页面.html": []byte(`<!doctype html><html><head><link rel="stylesheet" href="样式.css"><script src="逻辑.js"></script></head><body><h1 id="title">中文页面</h1><img id="picture" src="图标.png"><div style="height:3000px">滚动区域</div></body></html>`),
		"样式.css":  []byte(`h1{color:rgb(18, 52, 86)} body{margin:0}`),
		"逻辑.js":   []byte(`window.externalScriptLoaded="中文脚本"`),
		"图标.png":  applicationIconPNG,
	}
	for name, data := range fixtures {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := openSessionStore(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	m := newEditorApp()
	store.attach(m)
	originalPath := filepath.Join(directory, "页面.html")
	m.load(originalPath)
	tab := m.active
	m.windowFrame = gfx.Rect{X: 50, Y: 50, Width: 1100, Height: 760}
	var failure error
	completed := false
	phase := 0
	stop := make(chan struct{})
	defer close(stop)
	err = app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		finish := func(err error) {
			if completed {
				return
			}
			completed = true
			failure = err
			tab.html.close()
			application.Quit()
		}
		deadline := time.Now().Add(30 * time.Second)
		go func() {
			ticker := time.NewTicker(80 * time.Millisecond)
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
							finish(fmt.Errorf("WebView2 timeout: phase=%d ready=%v scroll=%v error=%s", phase, tab.html.ready, tab.html.scroll, tab.html.errorText))
							return
						}
						if tab.html.errorText != "" {
							finish(fmt.Errorf("WebView2: %s", tab.html.errorText))
							return
						}
						switch phase {
						case 0:
							if !tab.html.ready {
								return
							}
							phase = 1
							tab.html.view.Eval(`(()=>{const ok=document.getElementById('title').textContent==='中文页面'&&getComputedStyle(document.getElementById('title')).color==='rgb(18, 52, 86)'&&window.externalScriptLoaded==='中文脚本'&&document.getElementById('picture').naturalWidth>0;window.scrollTo(0,ok?731:11)})();`)
						case 1:
							if tab.html.scroll.Y == 11 {
								finish(fmt.Errorf("relative CSS/JS/image or Chinese DOM check failed"))
								return
							}
							if tab.html.scroll.Y != 731 {
								return
							}
							// Apply a model proposal through the same path as the AI review dialog.
							base := tab.source.text.String()
							target := aiTarget{
								file: aiFileContext{ID: tab.id, Name: tab.title(), Format: "HTML", Text: base},
								path: tab.doc.path, base: base, end: len(base),
							}
							change := aiChange{target: target, value: strings.Replace(base, "中文页面", "修改后的中文页面", 1)}
							if err := m.applyAIChange(&change); err != nil {
								finish(err)
								return
							}
							phase = 2
						case 2:
							if !tab.html.ready {
								return
							}
							phase = 3
							tab.html.view.Eval(`(()=>{const ok=document.getElementById('title').textContent==='修改后的中文页面'&&getComputedStyle(document.getElementById('title')).color==='rgb(18, 52, 86)'&&window.externalScriptLoaded==='中文脚本'&&document.getElementById('picture').naturalWidth>0;window.scrollTo(0,ok?911:12)})();`)
						case 3:
							if tab.html.scroll.Y == 12 {
								finish(fmt.Errorf("edited HTML refresh check failed"))
								return
							}
							if tab.html.scroll.Y != 911 {
								return
							}
							if err := store.save(); err != nil {
								finish(err)
								return
							}
							restored, err := openSessionStore(dataDirectory)
							if err != nil {
								finish(err)
								return
							}
							recovered := restored.windows[0].active
							if !recovered.modified() || recovered.html.scroll.Y != 911 || recovered.source.text.String() != tab.source.text.String() {
								finish(fmt.Errorf("native draft/scroll session restore failed"))
								return
							}
							original, err := os.ReadFile(originalPath)
							if err != nil || string(original) != string(fixtures["页面.html"]) {
								finish(fmt.Errorf("preview changed original file"))
								return
							}
							if _, err := os.Stat(filepath.Join(dataDirectory, "WebView2")); err != nil {
								finish(err)
								return
							}
							tab.html.close()
							m = restored.windows[0]
							m.maximized = true
							tab = recovered
							if err := openEditorWindow(application, m); err != nil {
								finish(err)
								return
							}
							phase = 4
						case 4:
							if !tab.html.ready {
								return
							}
							phase = 5
							tab.html.view.Eval(`(()=>{const ok=document.getElementById('title').textContent==='修改后的中文页面'&&scrollY===911;window.scrollTo(0,ok?1151:13)})();`)
						case 5:
							if tab.html.scroll.Y == 13 {
								finish(fmt.Errorf("reopened native page lost content or scroll"))
								return
							}
							if tab.html.scroll.Y != 1151 {
								return
							}
							handle, _, _ := editorUser.NewProc("GetActiveWindow").Call()
							zoomed, _, _ := editorUser.NewProc("IsZoomed").Call(handle)
							if zoomed == 0 || !m.maximized {
								finish(fmt.Errorf("restored window did not maximize"))
								return
							}
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
	if !completed {
		t.Fatal("native loop exited before validation")
	}
	if failure != nil {
		t.Fatal(failure)
	}
	t.Log("真实 WebView2：中文 DOM、相对 CSS/JavaScript/图片、未保存内容刷新、预览滚动和会话恢复通过；原文件不变，浏览器数据位于 D 盘测试目录。")
}
