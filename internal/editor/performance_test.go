package editor

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// Exercise actual UI events over 5,000 JSON objects, including UTF-8 values.
func BenchmarkInteractions(b *testing.B) {
	raw := "[" + strings.Repeat(`{"name":"中文配置","enabled":true,"port":8080},`, 4999) + `{"name":"中文配置","enabled":true,"port":8080}]`
	source, _, err := transformJSON(raw, "格式化", 4)
	if err != nil {
		b.Fatal(err)
	}
	for _, action := range []string{"Frame", "Search", "Tabs", "Menu", "Drag"} {
		b.Run(action, func(b *testing.B) {
			m := newEditorApp()
			m.showFiles = false
			first := m.addTab(document{saved: source, eol: "LF"}, "large.json")
			m.addTab(document{saved: source, eol: "LF"}, "other.json")

			page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
			session, err := headless.Open(ui.Configure(page, app.Config{Width: 1440, Height: 900}), nil, headless.Options{Scale: 1})
			if err != nil {
				b.Fatal(err)
			}
			defer session.Close()
			check := func(err error) {
				if err != nil {
					b.Fatal(err)
				}
			}
			m.activate(first)
			check(session.Frame())
			first.openSearch()
			first.search.query.Insert("中文")
			check(session.Frame())
			header, err := session.Locate("拖动查找窗口")
			check(err)
			x, y := header.Bounds.X+20, header.Bounds.Y+10
			if action == "Drag" {
				check(session.Driver().Press(x, y, gfx.PointerButtonPrimary))
			}
			if action == "Tabs" || action == "Menu" {
				first.search.close()
				check(session.Frame())
			}
			var menuX, menuY float32
			if action == "Menu" {
				menu, err := session.Locate("主菜单")
				check(err)
				menuX, menuY = menu.Bounds.X+16, menu.Bounds.Y+16
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				switch action {
				case "Frame":
					check(session.Frame())
				case "Search":
					first.search.query.SetSelection(text.Range{Start: 0, End: first.search.query.RuneCount()})
					if i%2 == 0 {
						check(session.Type("中文"))
					} else {
						check(session.Type("配置"))
					}
				case "Tabs":
					check(session.Driver().Key(gfx.KeyTab, gfx.ModifierControl))
				case "Menu":
					if m.fileMenu {
						check(session.Driver().Key(gfx.KeyEscape, 0))
					} else {
						check(session.Driver().Click(menuX, menuY))
					}
				case "Drag":
					check(session.Driver().Move(x-float32(i%100), y+float32(i%70)))
				}
			}
			b.StopTimer()
			if action == "Drag" {
				check(session.Driver().Release(x, y, gfx.PointerButtonPrimary))
			}
		})
	}
}
