package editor

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func TestFindDialogVisibleTitleDrag(t *testing.T) {
	for _, size := range []gfx.Size{{Width: 1440, Height: 900}, {Width: 900, Height: 560}} {
		for _, scale := range []float64{1, 2} {
			t.Run(fmt.Sprintf("%gx%g-scale%g", size.Width, size.Height, scale), func(t *testing.T) {
				m := newTestEditorWithDocument()
				m.setLightMode(scale == 2)
				tab := m.active
				source := "查找标题栏 中文"
				tab.source.replace(source)
				page := ui.NewPage(ui.PageConfig{View: m.page, Theme: &m.theme})
				session, err := headless.Open(ui.Configure(page, app.Config{Width: int(size.Width), Height: int(size.Height)}), nil, headless.Options{Scale: scale})
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				check := func(err error) {
					t.Helper()
					if err != nil {
						t.Fatal(err)
					}
				}
				openSearchForTest(t, session, "中文")
				checkCentered := func(width, height float32) {
					t.Helper()
					bounds := tab.search.bounds
					if bounds.X != (width-bounds.Width)/2 || bounds.Y != (height-bounds.Height)/2 {
						t.Fatalf("find dialog is not centered: viewport=%gx%g bounds=%+v", width, height, bounds)
					}
				}
				checkCentered(size.Width, size.Height)
				drag := func(point, delta gfx.Point) {
					t.Helper()
					before := tab.search.bounds
					x, y := before.X+point.X, before.Y+point.Y
					check(session.Driver().Press(x, y, gfx.PointerButtonPrimary))
					if !tab.search.dragging {
						t.Fatalf("visible title at %+v did not start dragging", point)
					}
					check(session.Driver().Move(x+delta.X, y+delta.Y))
					check(session.Driver().Release(x+delta.X, y+delta.Y, gfx.PointerButtonPrimary))
					wantX := max(12, min(before.X+delta.X, size.Width-before.Width-12))
					wantY := max(12, min(before.Y+delta.Y, size.Height-before.Height-12))
					if tab.search.bounds.X != wantX || tab.search.bounds.Y != wantY || tab.search.dragging {
						t.Fatal("title drag failed")
					}
				}
				// 按可见标题区取点，避免只在控件自己报告的命中框内验证。
				for _, point := range []gfx.Point{{X: 80, Y: 29}, {X: 8, Y: 8}, {X: 320, Y: 8}, {X: 600, Y: 44}, {X: 670, Y: 8}} {
					drag(point, gfx.Point{X: -24, Y: 18})
				}
				drag(gfx.Point{X: 80, Y: 29}, gfx.Point{X: -2000, Y: -2000})
				drag(gfx.Point{X: 8, Y: 8}, gfx.Point{X: 80, Y: 70})
				before := tab.search.bounds
				// 中间正文不是拖动手柄，输入框和按钮仍使用各自的交互。
				x, y := before.X+300, before.Y+175
				check(session.Driver().Press(x, y, gfx.PointerButtonPrimary))
				check(session.Driver().Move(x+15, y+15))
				check(session.Driver().Release(x+15, y+15, gfx.PointerButtonPrimary))
				if tab.search.dragging || tab.search.bounds != before {
					t.Fatal("dialog body incorrectly dragged")
				}
				check(session.ClickLabel("搜索内容"))
				check(session.Compose("zhongwen"))
				check(session.Type("中文"))
				check(session.ClickLabel("计数"))
				if tab.search.dragging || tab.search.bounds != before || tab.source.text.String() != source {
					t.Fatal("controls moved dialog or changed source")
				}
				check(session.ClickLabel("关闭搜索"))
				if tab.search.open || tab.search.dragging {
					t.Fatal("close button started dragging")
				}
				// 每次重新打开居中；浮窗已打开时重复 Ctrl+F 只聚焦输入，不突然跳位。
				_, err = session.Driver().ShortcutChord(gfx.ModifierControl, 'f')
				check(err)
				checkCentered(size.Width, size.Height)
				drag(gfx.Point{X: 80, Y: 29}, gfx.Point{X: -30, Y: 20})
				dragged := tab.search.bounds
				_, err = session.Driver().ShortcutChord(gfx.ModifierControl, 'f')
				check(err)
				if tab.search.bounds != dragged {
					t.Fatal("refocusing an open dialog moved it")
				}
				check(session.ClickLabel("关闭搜索"))
				check(session.Resize(1000, 700))
				m.showFiles = true
				_, err = session.Driver().ShortcutChord(gfx.ModifierControl, 'f')
				check(err)
				checkCentered(1000, 700)
			})
		}
	}
}
