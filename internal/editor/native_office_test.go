package editor

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/xuri/excelize/v2"
)

const checkWordTitleAndZoomJS = `(() => {
	try {
		if (document.querySelector("#name").textContent !== "中文.docx" || document.querySelector("#word-zoom").hidden) {
			throw new Error("Word title or zoom controls were hidden");
		}
		const editor = window.office.editor;
		const initial = editor.getZoom();
		document.querySelector("#word-larger").click();
		const enlarged = editor.getZoom();
		document.querySelector("#word-smaller").click();
		if (!(enlarged > initial && editor.getZoom() < enlarged)) {
			throw new Error("Word zoom buttons did not change the scale");
		}
		document.querySelector("#word-fit").click();
		if (editor.getZoomState().mode !== "fit-width") {
			throw new Error("Word did not fit the page width");
		}
	} catch (error) {
		window.viva.postMessage(JSON.stringify({kind: "error", message: error.message}));
	}
})()`

func TestNativeOfficeEditing(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native Office integration is opt-in")
	}
	directory := t.TempDir()
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", filepath.Join(directory, "WebView2"))
	store, err := openSessionStore(filepath.Join(directory, "data"))
	if err != nil {
		t.Fatal(err)
	}
	sheetPath, wordPath := filepath.Join(directory, "中文.xlsx"), filepath.Join(directory, "中文.docx")
	os.WriteFile(sheetPath, sampleWorkbook(t), 0600)
	os.WriteFile(wordPath, sampleDocx(t), 0600)
	m := newEditorApp()
	store.attach(m)
	m.load(sheetPath)
	sheetTab := m.active
	m.load(wordPath)
	wordTab := m.active
	m.activate(sheetTab)
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
			sheetTab.office.close()
			wordTab.office.close()
			application.Quit()
		}
		flushUnchanged := func(office *officeDocument, next func()) {
			office.mu.Lock()
			previous := office.received
			office.mu.Unlock()
			office.flush(func(err error) {
				if err != nil {
					finish(err)
					return
				}
				office.mu.Lock()
				current := office.received
				office.mu.Unlock()
				if len(current) != len(previous) || (len(current) > 0 && &current[0] != &previous[0]) {
					finish(fmt.Errorf("unchanged Office flush exported a duplicate document"))
					return
				}
				if office.pending != nil || office.flushTimer != nil {
					finish(fmt.Errorf("Office flush retained its completion timer"))
					return
				}
				next()
			})
		}
		deadline := time.Now().Add(80 * time.Second)
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
							finish(fmt.Errorf("Office timeout phase=%d, sheet=%s, word=%s", phase, sheetTab.office.errorText, wordTab.office.errorText))
							return
						}
						if m.showError && phase != 10 {
							finish(fmt.Errorf("Office error: %s", m.errorText))
							return
						}
						switch phase {
						case 0:
							if !sheetTab.office.ready {
								return
							}
							phase = 11
							flushUnchanged(sheetTab.office, func() {
								phase = 10
								sheetTab.office.view.Eval("document.dispatchEvent(new CompositionEvent('compositionstart',{data:'zhong'}));window.viva.postMessage(JSON.stringify({kind:'save'}))")
							})
						case 10:
							if !m.showError {
								return
							}
							if !strings.Contains(m.errorText, "中文选词") {
								finish(fmt.Errorf("composition save guard failed: %s", m.errorText))
								return
							}
							m.showError = false
							phase = 1
							sheetTab.office.view.Eval("document.dispatchEvent(new CompositionEvent('compositionend',{data:'中文'}))")
							sheetTab.office.view.Eval(`try { const sheet=window.office.workbook.getActiveSheet();sheet.getRange('A2').setValue(25);sheet.getRange('C2').setValue('=A2*B2'); }catch(e){window.viva.postMessage(JSON.stringify({kind:'error',message:String(e)}))}`)
						case 1:
							if !sheetTab.modified() {
								return
							}
							phase = 2
							m.saveOfficeTab(sheetTab, false, func() {
								data, e := os.ReadFile(sheetPath)
								if e != nil {
									finish(e)
									return
								}
								book, e := excelize.OpenReader(bytes.NewReader(data))
								if e != nil {
									finish(e)
									return
								}
								defer book.Close()
								value, e := book.CalcCellValue("Sheet1", "C2")
								if e != nil || value != "75" {
									finish(fmt.Errorf("native formula %q: %v", value, e))
									return
								}
								if !hasWordEditor {
									m.activate(wordTab)
									if wordTab.office.view != nil || wordTab.office.server != nil {
										finish(fmt.Errorf("public release started a Word editor"))
										return
									}
									finish(nil)
									return
								}
								m.setLightMode(true)
								m.activate(wordTab)
								phase = 3
							})
						case 3:
							if !wordTab.office.ready {
								return
							}
							phase = 12
							wordTab.office.view.Eval(`if (!document.documentElement.classList.contains('light') || getComputedStyle(document.querySelector('.superdoc-page')).filter !== 'none') { window.viva.postMessage(JSON.stringify({kind:'error',message:'Word light paper did not follow the theme'})) }`)
							wordTab.office.view.Eval(checkWordTitleAndZoomJS)
							m.setLightMode(false)
							flushUnchanged(wordTab.office, func() {
								phase = 4
								wordTab.office.view.Eval(`try { if (getComputedStyle(document.querySelector('.superdoc-page')).filter === 'none') throw new Error('Word dark paper did not follow the theme'); window.office.editor.activeEditor.doc.insert({value:'原生中文保存验证',type:'text'}); }catch(e){window.viva.postMessage(JSON.stringify({kind:'error',message:String(e)}))}`)
							})
						case 4:
							if !wordTab.modified() {
								return
							}
							phase = 5
							m.saveOfficeTab(wordTab, false, func() {
								data, e := os.ReadFile(wordPath)
								if e != nil {
									finish(e)
									return
								}
								body := string(zipPart(t, data, "word/document.xml"))
								if !strings.Contains(body, "原生中文保存验证") || !strings.Contains(body, "tbl") || !strings.Contains(body, "drawing") {
									finish(fmt.Errorf("Word edit/table/image lost"))
									return
								}
								if !strings.Contains(strings.ToLower(body), "235a97") {
									finish(fmt.Errorf("Word dark display changed the saved text color"))
									return
								}
								if len(zipPart(t, data, "word/media/image1.png")) == 0 {
									finish(fmt.Errorf("Word image lost"))
									return
								}
								flushUnchanged(wordTab.office, func() {
									wordTab.office.view.Eval(`window.office.editor.activeEditor.doc.insert({value:'会话恢复草稿',type:'text'})`)
									phase = 6
								})
							})
						case 6:
							if !wordTab.modified() {
								return
							}
							phase = 7
							m.requestClose(func() {
								restored, e := openSessionStore(store.directory)
								if e != nil {
									finish(e)
									return
								}
								tab := restored.windows[0].active
								if tab.office == nil || !tab.modified() {
									finish(fmt.Errorf("Office dirty session not restored"))
									return
								}
								output, e := tab.office.output()
								if e != nil {
									finish(e)
									return
								}
								if !strings.Contains(string(zipPart(t, output, "word/document.xml")), "会话恢复草稿") {
									finish(fmt.Errorf("Office last edit missing from session"))
									return
								}
								finish(nil)
							})
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
		t.Fatal("native loop stopped early")
	}
	if failure != nil {
		t.Fatal(failure)
	}
	if hasWordEditor {
		t.Log("真实 WebView2：Word 标题与缩放、XLSX 公式编辑保存、DOCX 中文编辑保存、表格图片保留、退出时最后一次修改与草稿恢复、未编辑/重复保存不重新导出通过。")
		return
	}
	t.Log("真实 WebView2：公开版 XLSX 公式编辑保存、中文输入保护及未编辑时不重新导出通过。")
}
