package editor

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

var errLegacyOfficeFormat = errors.New("请先用 Office 将旧格式另存为 .xlsx 或 .docx 后打开")

type officeSession struct {
	Kind     string
	Original []byte
	Snapshot []byte `json:",omitempty"`
	Dirty    bool
}
type officeDocument struct {
	state            officeSession
	workbook         func() (*officeWorkbook, error)
	view             *app.WebView
	server           *http.Server
	url              string
	ready            bool
	errorText        string
	pending          func(error)
	flushTimer       *time.Timer
	request          int
	revision         int
	mu               sync.Mutex
	received         []byte
	serverContent    []byte
	serverClosed     bool
	receivedRevision int
	post             func(func())
	invalidate       func()
}

func officeKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx":
		return "xlsx"
	case ".docx":
		return "docx"
	}
	return ""
}
func readOffice(path string) (document, *officeDocument, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return document{}, nil, err
	}
	doc, office, err := decodeOffice(filepath.Base(path), data)
	if err == nil {
		doc.path = path
	}
	return doc, office, err
}

func decodeOffice(name string, data []byte) (document, *officeDocument, error) {
	office, err := restoreOffice(officeSession{Kind: officeKind(name), Original: data}, name)
	if err != nil {
		return document{}, nil, err
	}
	return document{diskHash: sha256.Sum256(data)}, office, nil
}
func restoreOffice(state officeSession, name string) (*officeDocument, error) {
	// Older sessions stored the original DOCX plus a full exported ZIP. The latest ZIP is
	// self-contained; disk conflict detection uses document.diskHash, not the old bytes.
	if state.Kind == "docx" && len(state.Snapshot) > 0 {
		state.Original, state.Snapshot = state.Snapshot, nil
	}
	office := &officeDocument{state: state}
	if state.Kind == "xlsx" {
		if _, err := zip.NewReader(bytes.NewReader(state.Original), int64(len(state.Original))); err != nil {
			return nil, fmt.Errorf("无法读取 XLSX 工作簿：%w", err)
		}
		office.workbook = sync.OnceValues(func() (*officeWorkbook, error) {
			return readWorkbook(state.Original, name)
		})
	} else if err := validateDocx(state.Original); err != nil {
		return nil, err
	}
	return office, nil
}
func (office *officeDocument) output() ([]byte, error) {
	if len(office.state.Snapshot) == 0 {
		return office.state.Original, nil
	}
	if office.state.Kind == "docx" {
		if err := validateDocx(office.state.Snapshot); err != nil {
			return nil, err
		}
		return office.state.Snapshot, nil
	}
	var current officeWorkbook
	if err := json.Unmarshal(office.state.Snapshot, &current); err != nil {
		return nil, err
	}
	baseline, err := office.workbook()
	if err != nil {
		return nil, err
	}
	return writeWorkbook(office.state.Original, baseline, &current)
}
func (office *officeDocument) serve(name string) error {
	kind := office.state.Kind
	content := office.state.Snapshot
	if len(content) == 0 && kind == "docx" {
		content = office.state.Original
	}
	office.mu.Lock()
	office.serverContent, office.serverClosed = content, false
	office.mu.Unlock()
	info := struct {
		Name     string `json:"name"`
		Kind     string `json:"kind"`
		Dirty    bool   `json:"dirty"`
		Revision int    `json:"revision"`
	}{Name: name, Kind: office.state.Kind, Dirty: office.state.Dirty}
	assets, _ := fs.Sub(officeAssets, officeAssetDirectory)
	server, address, err := serveLocalDocument(assets, func(w http.ResponseWriter, r *http.Request, route string) bool {
		// All Office code and document resources stay on this loopback origin.
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data: blob:; connect-src 'self' blob:; worker-src 'self' blob:; object-src 'none'; frame-ancestors 'none'")
		host := r.Host
		if route == "snapshot" {
			if r.Method != "POST" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return true
			}
			if origin := r.Header.Get("Origin"); origin != "http://"+host {
				http.Error(w, "不允许跨站写入", http.StatusForbidden)
				return true
			}
			revision, e := strconv.Atoi(r.URL.Query().Get("revision"))
			if e != nil || revision < 0 {
				http.Error(w, "无效的文档版本", 400)
				return true
			}
			body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 128<<20))
			if e != nil {
				http.Error(w, e.Error(), 400)
				return true
			}
			if office.state.Kind == "docx" {
				e = validateDocx(body)
			} else {
				var book officeWorkbook
				e = json.Unmarshal(body, &book)
				if e == nil && (len(book.Sheets) == 0 || len(book.Order) == 0) {
					e = errors.New("工作簿内容为空")
				}
			}
			if e != nil {
				http.Error(w, e.Error(), 400)
				return true
			}
			office.mu.Lock()
			if office.serverClosed {
				office.mu.Unlock()
				http.Error(w, "文档已关闭", http.StatusServiceUnavailable)
				return true
			}
			if revision >= office.receivedRevision {
				office.received, office.receivedRevision = body, revision
				office.serverContent = body
			}
			office.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return true
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return true
		}
		if route == "" || route == "index.html" {
			serveEditorPage(w, r, assets, kind+".html")
			return true
		}
		if route == "info" {
			w.Header().Set("Content-Type", "application/json")
			metadata := info
			office.mu.Lock()
			metadata.Revision = office.receivedRevision
			office.mu.Unlock()
			json.NewEncoder(w).Encode(metadata)
			return true
		}
		if route == "document" {
			office.mu.Lock()
			current := office.serverContent
			office.mu.Unlock()
			if kind == "xlsx" {
				w.Header().Set("Content-Type", "application/json")
				if len(current) == 0 {
					book, err := office.workbook()
					if err != nil {
						http.Error(w, err.Error(), http.StatusUnprocessableEntity)
						return true
					}
					_ = json.NewEncoder(w).Encode(book)
					return true
				}
			} else {
				w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			}
			_, _ = w.Write(current)
			return true
		}
		return false
	})
	if err != nil {
		return err
	}
	office.server, office.url = server, address
	return nil
}
func (office *officeDocument) setLightMode(light bool) {
	if office != nil && office.view != nil {
		office.view.Eval("window.office?.setTheme(" + strconv.FormatBool(light) + ")")
	}
}

func (m *editorApp) openOffice(t *editorTab) {
	office := t.office
	if !m.webReady || office == nil || office.view != nil {
		return
	}
	if !hasWordEditor && office.state.Kind == "docx" {
		return
	}
	if office.server == nil {
		if err := office.serve(t.title()); err != nil {
			office.errorText = err.Error()
			return
		}
	}
	office.post, office.invalidate = m.post, m.invalidator.Invalidate
	address := office.url
	if m.light {
		address += "?theme=light"
	}
	view, err := m.webHost.Open(app.WebViewConfig{URL: address, Ephemeral: true, TopCornerRadius: 12, BridgeMainFrameOnly: true,
		LetterShortcut: m.openLinkWebShortcut,
		Navigate:       func(n app.WebNavigation) bool { return strings.HasPrefix(n.URL, office.url) },
		OnMessage: func(raw string) {
			defer m.invalidator.Invalidate()
			var message struct {
				Kind              string
				Message           string
				Request, Revision int
			}
			if json.Unmarshal([]byte(raw), &message) != nil {
				return
			}
			switch message.Kind {
			case "ready":
				office.ready = true
				office.setLightMode(m.light)
				office.errorText = ""
				m.status = "Office 编辑器已就绪"
			case "dirty":
				office.state.Dirty = true
				office.revision = message.Revision
			case "open":
				m.open()
			case "new":
				m.newDocument()
			case "new-window":
				if m.newWindow != nil {
					m.newWindow()
				}
			case "close":
				m.closeTab(t)
			case "next-tab", "previous-tab":
				step := 1
				if message.Kind == "previous-tab" {
					step = -1
				}
				m.stepTab(step)
			case "save":
				m.saveOfficeTab(t, false, nil)
			case "save-as":
				m.saveOfficeTab(t, true, nil)
			case "snapshot":
				office.acceptSnapshot()
				office.finishFlush(message.Request, nil)
			case "flushed":
				office.finishFlush(message.Request, nil)
			case "error":
				office.errorText = message.Message
				m.status = "Office：" + message.Message
				office.finishFlush(message.Request, errors.New(message.Message))
			}
		},
	})
	if err != nil {
		office.errorText = fmt.Sprintf("无法启动 Office 编辑器，请检查 WebView2：%v", err)
		return
	}
	office.view = view
}

// Snapshot buffers are immutable after publication. Replacing slice headers lets session
// writes and the loopback server share the same bytes without retaining earlier DOCX exports.
func (office *officeDocument) acceptSnapshot() {
	office.mu.Lock()
	received := office.received
	office.mu.Unlock()
	if len(received) == 0 {
		return
	}
	if office.state.Kind == "docx" {
		office.state.Original, office.state.Snapshot = received, nil
	} else {
		office.state.Snapshot = received
	}
}
func (office *officeDocument) finishFlush(request int, err error) {
	if request != office.request || office.pending == nil {
		return
	}
	if office.flushTimer != nil {
		office.flushTimer.Stop()
		office.flushTimer = nil
	}
	callback := office.pending
	office.pending = nil
	if office.view != nil {
		office.view.Eval("window.office.release()")
	}
	callback(err)
}
func (office *officeDocument) flush(done func(error)) {
	if office.pending != nil {
		done(errors.New("正在处理文档，请稍后再试"))
		return
	}
	if office.view == nil || !office.ready {
		done(nil)
		return
	}
	office.request++
	office.pending = done
	request := office.request
	if office.post != nil {
		post := office.post
		office.flushTimer = time.AfterFunc(30*time.Second, func() {
			post(func() {
				if office.request != request || office.pending == nil {
					return
				}
				office.finishFlush(request, errors.New("文档导出超时，原文件未修改，请重试"))
				office.invalidate()
			})
		})
	}
	office.view.Eval(fmt.Sprintf("window.office.flush(%d)", office.request))
}
func (m *editorApp) saveOfficeTab(t *editorTab, saveAs bool, done func()) {
	office := t.office
	if office.view != nil && !office.ready {
		m.fail(errors.New("文档尚未加载完成，请稍后再保存"))
		return
	}
	office.flush(func(err error) {
		if err != nil {
			m.fail(err)
			return
		}
		path := t.doc.path
		if saveAs || path == "" {
			path, err = app.SaveFile(app.SaveFileOptions{Title: "保存 " + t.title(), SuggestedName: t.title(), Filters: []app.FileFilter{{Name: strings.ToUpper(office.state.Kind) + " 文档", Pattern: "*." + office.state.Kind}}})
			if errors.Is(err, app.ErrCanceled) {
				return
			}
			if err != nil {
				m.fail(err)
				return
			}
			if filepath.Ext(path) == "" {
				path += "." + office.state.Kind
			}
		}
		if officeKind(path) != office.state.Kind {
			m.fail(fmt.Errorf("请保存为 .%s 文件", office.state.Kind))
			return
		}
		path, err = filepath.Abs(path)
		if err != nil {
			m.fail(err)
			return
		}
		for _, other := range m.tabs {
			if other != t && strings.EqualFold(other.doc.path, path) {
				m.fail(errors.New("该文件已在另一个标签中打开，请使用其他文件名"))
				return
			}
		}
		if err = saveOfficeFile(&t.doc, office, path, m.documentHistory()); err != nil {
			m.fail(err)
			return
		}
		m.rememberPath(path, false)
		m.status = "已保存 " + filepath.Base(path)
		office.errorText = ""
		if office.view != nil {
			office.view.Eval(fmt.Sprintf("window.office.ack(%d)", office.revision))
		}
		if done != nil {
			done()
		}
	})
}
func saveOfficeFile(doc *document, office *officeDocument, path string, history *localHistoryStore) error {
	original, err := doc.checkDisk(context.Background(), path)
	if err != nil {
		return err
	}
	data, err := office.output()
	if err != nil {
		return err
	}
	digest, err := writeDocumentBytes(path, data, original, history)
	if err != nil {
		return err
	}
	doc.path, doc.diskHash = path, digest
	office.state.Dirty = false
	return nil
}
func (office *officeDocument) close() {
	if office == nil {
		return
	}
	if office.flushTimer != nil {
		office.flushTimer.Stop()
		office.flushTimer = nil
	}
	office.pending = nil
	office.request++
	office.ready = false
	office.mu.Lock()
	office.serverClosed = true
	office.serverContent, office.received = nil, nil
	office.mu.Unlock()
	if office.view != nil {
		office.view.Close()
		office.view = nil
	}
	if office.server != nil {
		office.server.Close()
		office.server = nil
	}
}
func (m *editorApp) officeView(c ui.C, t *editorTab, blocked bool) {
	office := t.office
	if !hasWordEditor && office.state.Kind == "docx" {
		ui.Text(c, "Word 文档请用系统关联的办公软件打开。").FillWidth().Pad(24).Muted()
		if ui.Button(c, "在系统中打开").MarginX(24).Disabled(blocked || t.doc.path == "").Clicked() {
			c.After(func() {
				if err := app.OpenPath(t.doc.path); err != nil {
					m.fail(err)
				}
			})
		}
		return
	}
	if m.webReady && office.view == nil && office.errorText == "" {
		c.After(func() { m.openOffice(t) })
	}
	if office.view != nil && !blocked {
		ui.Slot(c).FillWidth().Grow(1).NativeView(office.view.ID())
		return
	}
	message := "正在打开 " + strings.ToUpper(office.state.Kind) + " 编辑器…"
	if office.errorText != "" {
		message = office.errorText
	}
	if blocked {
		message = "请完成当前操作"
	}
	ui.Text(c, message).FillWidth().Pad(24).Muted()
	if office.view == nil && office.errorText != "" && ui.Button(c, "重试打开文档").MarginX(24).Clicked() {
		c.After(func() { office.errorText = ""; m.openOffice(t) })
	}
}
func (m *editorApp) flushOfficeTabs(index int, done func(error)) {
	for index < len(m.tabs) {
		tab := m.tabs[index]
		index++
		if tab.office != nil && tab.office.view != nil {
			tab.office.flush(func(err error) {
				if err != nil {
					done(fmt.Errorf("无法保存 %s 的会话：%w", tab.title(), err))
					return
				}
				m.flushOfficeTabs(index, done)
			})
			return
		}
	}
	done(nil)
}
func (office *officeDocument) snapshotSession() *officeSession {
	state := office.state
	// A session owns slice headers, not a mutable backing buffer. Updates replace the
	// entire validated export, so background serialization can safely share these bytes.
	if state.Kind == "docx" && len(state.Snapshot) > 0 {
		state.Original, state.Snapshot = state.Snapshot, nil
	}
	return &state
}
