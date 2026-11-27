package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuickActionsOpenSaveAndFind(t *testing.T) {
	m := newEditorApp()
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(s.Resize(900, 560))
	path := filepath.Join(t.TempDir(), "中文配置.txt")
	check(os.WriteFile(path, []byte("已有内容"), 0600))
	check(s.Driver().QueueOpenFiles([]string{path}, nil))
	check(s.ClickLabel("打开文件"))
	if m.active.doc.path != path || m.active.source.text.String() != "已有内容" {
		t.Fatal("empty document open action failed")
	}
	m.active.source.replace("修改后的中文")
	check(s.Frame())
	check(s.ClickLabel("保存文件"))
	data, err := os.ReadFile(path)
	check(err)
	if string(data) != "修改后的中文" || m.active.modified() {
		t.Fatalf("quick save did not persist the active document: %q", data)
	}
	check(s.ClickLabel("查找内容"))
	if !m.active.search.open {
		t.Fatal("quick find did not open search")
	}
	check(s.ClickLabel("关闭搜索"))
	check(s.Compose("zhongwen"))
	check(s.Type("中文"))
	if m.active.source.text.String() != "中文修改后的中文" {
		t.Fatalf("quick actions did not return input focus: %q", m.active.source.text.String())
	}
}

func TestQuickActionsRespectModalAndFileKind(t *testing.T) {
	m := newTestEditorWithDocument()
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	checkDisabled := func(label string, want bool) {
		t.Helper()
		for _, hit := range s.Snapshot().Hits {
			if hit.Label == label {
				if hit.Disabled != want {
					t.Fatalf("%s disabled = %v, want %v", label, hit.Disabled, want)
				}
				return
			}
		}
		t.Fatalf("missing quick action: %s", label)
	}
	for _, blocked := range []*bool{&m.confirm, &m.showError, &m.showHistory} {
		*blocked = true
		check(s.Frame())
		checkDisabled("保存文件", true)
		checkDisabled("查找内容", true)
		*blocked = false
	}
	check(s.Frame())
	checkDisabled("保存文件", false)
	checkDisabled("查找内容", false)
	m.load("assets/icon.png")
	check(s.Frame())
	checkDisabled("保存文件", true)
	checkDisabled("查找内容", true)
	m.newDocument()
	m.active.office = &officeDocument{state: officeSession{Kind: "docx"}}
	check(s.Frame())
	checkDisabled("保存文件", false)
	checkDisabled("查找内容", true)
}
