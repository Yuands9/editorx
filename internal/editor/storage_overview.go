package editor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type editorStorageUsage struct {
	data, history, cache int64
}

type editorStorageScan struct {
	usage   editorStorageUsage
	cleared int64
}

func scanEditorStorage(ctx context.Context, directory string, clean bool) (editorStorageScan, error) {
	var scan editorStorageScan
	root, err := os.OpenRoot(directory)
	if errors.Is(err, os.ErrNotExist) {
		return scan, nil
	}
	if err != nil {
		return scan, err
	}
	defer root.Close()
	var firstError error
	recordError := func(err error) {
		if firstError == nil && !errors.Is(err, os.ErrNotExist) {
			firstError = err
		}
	}
	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			recordError(err)
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			recordError(err)
			return nil
		}
		top, _, nested := strings.Cut(path, "/")
		if nested && strings.EqualFold(top, localHistoryDirectory) {
			scan.usage.history += info.Size()
			return nil
		}
		if owner := editorCacheOwner(path); owner != "" {
			key := editorCacheKey(filepath.Join(directory, filepath.FromSlash(owner)))
			activeEditorCache.Lock()
			_, active := activeEditorCache.paths[key]
			if clean && !active {
				err = root.Remove(path)
			}
			activeEditorCache.Unlock()
			if !active {
				if clean && err == nil {
					scan.cleared += info.Size()
					return nil
				}
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				recordError(err)
				scan.usage.cache += info.Size()
			}
		}
		scan.usage.data += info.Size()
		return nil
	})
	return scan, errors.Join(firstError, err)
}

type storageOverview struct {
	usage           editorStorageUsage
	request         *ui.EffectState[editorStorageScan, int]
	ready, cleaning bool
	notice, problem string
}

func (state *storageOverview) startScan(directory string, clean bool) {
	state.cleaning, state.notice, state.problem = clean, "", ""
	state.request = ui.NewEffect[editorStorageScan, int](func(ctx context.Context) (editorStorageScan, error) {
		return scanEditorStorage(ctx, directory, clean)
	})
}

func (m *editorApp) storageOverview(c ui.C, page *localHistoryPage) {
	state := &page.overview
	if !state.ready && state.request == nil {
		state.startScan(m.dataDirectory, false)
	}
	if state.request != nil {
		ui.Effect(c.Part("scan"), state.request, 0)
		if scan, err, ready := state.request.Take(); ready {
			state.usage, state.ready, state.request = scan.usage, true, nil
			if state.cleaning && (err == nil || scan.cleared > 0) {
				state.notice = fmt.Sprintf(uiText("已清理 %s 缓存"), historySize(scan.cleared))
			}
			if err != nil {
				label := "存储统计不完整"
				if state.cleaning {
					label = "部分缓存无法清理"
				}
				state.problem = fmt.Sprintf(uiText("%s：%s"), uiText(label), err.Error())
			}
			state.cleaning = false
		}
	}
	total := state.usage.data + state.usage.history
	size := func(bytes int64) string {
		if !state.ready {
			return uiText("统计中…")
		}
		return historySize(bytes)
	}
	dataColor, historyColor := m.theme.Ring, m.theme.Success
	ui.Column(c.Part("total")).FillWidth().Pad(14).Gap(9).Radius(12).Border(1, m.theme.Border).Background(m.theme.Background).Children(func() {
		ui.Row(c.Part("heading")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, uiText("EditorX 总占用")).FontSize(13).Muted()
				ui.Text(c, size(total)).FontSize(24).Bold()
			})
			if ui.Button(c.Part("refresh"), uiText("刷新占用")).Icon(refreshIcon).Ghost().Height(32).
				Disabled(state.request != nil).Clicked() {
				c.After(func() { state.ready = false })
			}
		})
		ui.Row(c.Part("distribution")).FillWidth().Height(8).Radius(4).Background(m.theme.Muted).Clip().Draw(func(d *ui.DrawContext) {
			if total == 0 {
				return
			}
			left := d.Rect.Width * float32(float64(state.usage.data)/float64(total))
			d.Canvas.Fill(gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: left, Height: d.Rect.Height}, dataColor)
			d.Canvas.Fill(gfx.Rect{X: d.Rect.X + left, Y: d.Rect.Y, Width: d.Rect.Width - left, Height: d.Rect.Height}, historyColor)
		})
		ui.Row(c.Part("legend")).FillWidth().Gap(15).Items(ui.Center).Children(func() {
			for _, entry := range []struct {
				label string
				color gfx.Color
			}{{"应用数据", dataColor}, {"历史版本", historyColor}} {
				ui.Row(c.Key(entry.label)).Gap(5).Items(ui.Center).Children(func() {
					ui.Slot(c).Size(8, 8).Radius(4).Background(entry.color)
					ui.Text(c, uiText(entry.label)).Muted().FontSize(12)
				})
			}
		})
	})
	ui.Column(c.Part("data")).FillWidth().Pad(14).Gap(10).Radius(12).Border(1, m.theme.Border).Background(m.theme.Background).Children(func() {
		ui.Row(c.Part("data-heading")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
				ui.Text(c, uiText("应用数据")).FontSize(14).Bold()
				path := m.dataDirectory
				ui.Text(c, path).FontSize(12).Muted().Ellipsis().Hit(ui.ID(c.Part("data-path")), path).Tooltip(path)
			})
			ui.Text(c, size(state.usage.data)).FontSize(15).Bold().Shrink(0)
			if ui.Button(c.Part("browse"), uiText("打开数据目录")).Icon(folderOpenIcon).Ghost().Height(32).
				Disabled(m.dataDirectory == "").Clicked() {
				c.After(func() { m.openExplorerFolder(m.dataDirectory) })
			}
		})
		ui.Row(c.Part("cache-details")).FillWidth().Gap(12).Items(ui.Center).Children(func() {
			ui.Text(c, uiText("可清理 ")+size(state.usage.cache)).FontSize(12).Muted().Shrink(0)
			ui.Text(c, uiText("不可清理 ")+size(state.usage.data-state.usage.cache)).FontSize(12).Muted().Grow(1).MinWidth(0).Ellipsis()
			if ui.Button(c.Part("change"), uiText("更换位置…")).Outline().Height(32).
				Disabled(m.desktop == nil || m.desktop.dataLocation.path == "" || state.cleaning).Clicked() {
				c.After(m.chooseDataLocation)
			}
			label := "清理缓存"
			if state.cleaning {
				label = "正在清理…"
			}
			if ui.Button(c.Part("clean"), uiText(label)).Outline().Height(32).
				Tooltip(uiText("清理残留的 HTML 预览和 AI CLI 临时文件")).Disabled(state.request != nil || state.usage.cache == 0).Clicked() {
				c.After(func() { state.startScan(m.dataDirectory, true) })
			}
		})
		if state.notice != "" {
			ui.Text(c, state.notice).FontSize(12).Color(m.theme.Success)
		}
	})
	m.dataLocationPending(c.Part("location"))
	m.applicationSetting(c.Part("history"), uiText("历史版本"),
		fmt.Sprintf(uiText("只记录大小在 %d MiB 以内的版本。"), localHistoryFileSizeLimit>>20), func() {
			ui.Text(c, size(state.usage.history)).FontSize(15).Bold().Shrink(0)
			if ui.Button(c.Part("manage"), uiText("管理历史版本")).Icon(historyCompareIcon).Outline().Height(32).Disabled(state.cleaning).Clicked() {
				c.After(func() { page.manage = true })
			}
		})
	if state.problem != "" {
		ui.Wrap(c, state.problem).FillWidth().Muted().FontSize(12)
	}
}
