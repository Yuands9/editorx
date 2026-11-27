package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestEditorContextMenuEditing(t *testing.T) {
	const source = "alpha\n测试\n"
	m := newEditorApp()
	tab := m.addTab(document{saved: source, eol: "LF"}, "context.txt")
	editor := tab.source.text
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	openMenu := func() {
		t.Helper()
		area, found := session.AccessByRole(gfx.RoleTextField, "代码编辑区")
		if !found {
			t.Fatal("missing editor")
		}
		// The welcome buttons can cover the center of an empty editor.
		check(session.RightClick(area.Bounds.X+80, area.Bounds.Y+20))
	}
	click := func(label string) {
		t.Helper()
		check(session.ClickLabel("menuitem:" + label))
	}
	check(session.Resize(900, 560))
	check(session.ClickLabel("textfield:代码编辑区"))
	check(session.Keys("shift+f10"))
	for _, label := range []string{"剪切", "复制", "删除选区", "撤销", "重做"} {
		if item, found := session.AccessByRole(gfx.RoleMenuItem, label); !found || !item.Disabled {
			t.Fatalf("%q should be visible and disabled", label)
		}
	}
	click("全选")
	openMenu()
	for _, label := range []string{"去翻译", "整篇翻译", "识别到密码本…"} {
		if item, found := session.AccessByRole(gfx.RoleMenuItem, label); !found || item.Disabled {
			t.Fatalf("%q should be available with selected text", label)
		}
	}
	if preview := os.Getenv("VIVA_PREVIEW_DIR"); preview != "" {
		check(session.Screenshot(filepath.Join(preview, "editor-context-menu.png")))
	}
	click("剪切")
	if session.Driver().Clipboard() != source || editor.String() != "" {
		t.Fatalf("cut clipboard = %q, text = %q", session.Driver().Clipboard(), editor.String())
	}
	openMenu()
	click("撤销")
	if editor.String() != source {
		t.Fatal("undo did not restore the original text")
	}
	openMenu()
	click("重做")
	openMenu()
	click("粘贴")
	if editor.String() != source {
		t.Fatalf("paste text = %q", editor.String())
	}
	if !editor.SetSelections([]text.Range{{End: 2}, {Start: 4, End: 4}, {Start: 6, End: 8}}, 0) {
		t.Fatal("failed to select separate ranges")
	}
	check(session.Frame())
	openMenu()
	click("复制")
	if session.Driver().Clipboard() != "al\n测试" || editor.String() != source {
		t.Fatal("copy did not preserve the selected ranges")
	}
	openMenu()
	click("删除选区")
	if editor.String() != "pha\n\n" || session.Driver().Clipboard() != "al\n测试" {
		t.Fatalf("deleting a selection changed the clipboard or the unselected text: %q", editor.String())
	}
	openMenu()
	click("查找…")
	if !tab.search.open || tab.search.target != tab.source {
		t.Fatal("find did not open in the source pane")
	}
	check(session.ClickLabel("button:关闭搜索"))
	openMenu()
	click("替换…")
	if !tab.search.open || !tab.search.replacing {
		t.Fatal("replace did not open")
	}
	check(session.ClickLabel("button:关闭搜索"))
	openMenu()
	click("转到行…")
	if !m.gotoLine.open {
		t.Fatal("go to line did not open")
	}
}

func TestEditorContextMenuTargetsJSONComparison(t *testing.T) {
	const original = `{"original":true}`
	const pasted = `{"comparison":true}`
	m := newEditorApp()
	tab := m.addTab(document{saved: original, eol: "LF"}, "context.json")
	tab.jsonCompare = true
	session := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	check(session.ClickLabel("textfield:代码编辑区"))
	check(session.RightClickLabel("textfield:JSON 对比输入"))
	check(session.ClickLabel("menuitem:查找…"))
	if !tab.search.open || tab.search.target != tab.comparison {
		t.Fatal("find targeted the previously focused pane")
	}
	check(session.ClickLabel("button:关闭搜索"))
	session.Driver().SetClipboard(pasted)
	check(session.RightClickLabel("textfield:JSON 对比输入"))
	check(session.ClickLabel("menuitem:粘贴"))
	if tab.comparison.text.String() != formatJSONPaste(pasted, tab.jsonIndent) || tab.source.text.String() != original {
		t.Fatal("paste did not format the text in the clicked pane")
	}
}
