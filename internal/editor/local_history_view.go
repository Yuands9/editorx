package editor

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

type localHistoryPage struct {
	manage             bool
	overview           storageOverview
	loaded, rulesReady bool
	expanded           string
	filter, days       ui.Field
	versions           ui.Field
	catalog            localHistoryCatalog
	scan               *ui.EffectState[localHistoryCatalog, int]
	action             *ui.EffectState[string, int]
	list               ui.ListState
	selected           map[string]localHistoryVersion
	confirm, clearAll  bool
	notice, problem    string
}

func (m *editorApp) localHistorySettings(c ui.C) {
	store := m.documentHistory()
	if store == nil {
		ui.Text(c, uiText("无法定位 EditorX 数据目录")).Muted()
		return
	}
	state := &m.appSettings.history
	if !state.manage {
		m.storageOverview(c, state)
		return
	}
	state.update(c, store)
	ui.Row(c.Part("history-nav")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		if ui.Button(c.Part("back"), uiText("存储概览")).Icon(chevronLeftIcon).Ghost().Height(30).
			Disabled(state.action != nil).Clicked() {
			c.After(func() {
				state.manage = false
				state.overview = storageOverview{}
			})
		}
		ui.Text(c, uiText("历史版本")).FontSize(16).Bold()
	})
	m.historyRetentionSettings(c.Part("retention"), store, state)
	ui.Row(c.Part("summary")).FillWidth().Items(ui.Center).Gap(8).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, uiText("历史记录")).FontSize(14).Bold()
			ui.Text(c, fmt.Sprintf(uiText("%s · %d 个文件 · %d 个版本"), historySize(state.catalog.bytes),
				len(state.catalog.files), state.catalog.count)).FontSize(12).Muted()
		})
		if ui.Button(c.Part("delete"), fmt.Sprintf(uiText("清理所选 (%d)"), len(state.selected))).Outline().Height(32).
			Disabled(len(state.selected) == 0 || state.action != nil).Clicked() {
			state.confirm, state.clearAll = true, false
		}
		if ui.Button(c.Part("clear"), uiText("清空历史")).Ghost().Height(32).
			Disabled(state.catalog.count == 0 || state.action != nil).Clicked() {
			state.confirm, state.clearAll = true, true
		}
	})
	if state.notice != "" {
		ui.Wrap(c.Part("notice"), state.notice).FillWidth().FontSize(12).Color(m.theme.Success)
	}
	for index, problem := range []string{state.catalog.problem, state.problem} {
		if problem != "" {
			ui.Wrap(c.Part("problem").KeyInt(index), problem).FillWidth().FontSize(12).Color(m.theme.Danger)
		}
	}
	if state.action != nil {
		ui.Text(c, uiText("正在处理历史记录…")).Muted().FontSize(12)
	}
	m.historyRecords(c.Part("records"), state)
	m.historyCleanupDialog(c.Part("cleanup"), store, state)
}

func (state *localHistoryPage) update(c ui.C, store *localHistoryStore) {
	if state.action != nil {
		ui.Effect(c.Part("action"), state.action, 0)
		if message, err, ready := state.action.Take(); ready {
			state.action, state.notice, state.problem = nil, message, ""
			if err != nil {
				state.problem = err.Error()
			}
		}
	}
	revision := store.revision.Load()
	if state.scan == nil && (!state.loaded || state.catalog.revision != revision) {
		state.scan = ui.NewEffect[localHistoryCatalog, int](func(ctx context.Context) (localHistoryCatalog, error) {
			return store.catalog()
		})
	}
	if state.scan == nil {
		return
	}
	ui.Effect(c.Part("catalog"), state.scan, 0)
	if catalog, err, ready := state.scan.Take(); ready {
		state.scan, state.loaded = nil, true
		if err != nil {
			state.catalog.revision = revision
			state.catalog.problem = uiText("无法读取本地历史：") + err.Error()
			return
		}
		state.catalog = catalog
		if len(state.selected) != 0 {
			remaining := make(map[string]localHistoryVersion)
			for _, file := range catalog.files {
				for _, version := range file.versions {
					if key := version.key(); state.selected[key].name != "" {
						remaining[key] = version
					}
				}
			}
			state.selected = remaining
		}
	}
}

func (m *editorApp) historyRetentionSettings(c ui.C, store *localHistoryStore, state *localHistoryPage) {
	preferences := m.appSettings.draft.LocalHistory
	days, versions := preferences.limits()
	if !state.rulesReady {
		state.rulesReady = true
		state.days.SetText(strconv.Itoa(max(0, days)))
		state.versions.SetText(strconv.Itoa(max(0, versions)))
	}
	enabled := !preferences.Disabled
	if m.applicationSettingSwitch(c.Part("record"), uiText("记录本地历史"),
		fmt.Sprintf(uiText("保存改动时，压缩保存大小在 %d MiB 以内的旧版本。"), localHistoryFileSizeLimit>>20), &enabled) {
		m.appSettings.draft.LocalHistory.Disabled = !enabled
		c.After(m.saveApplicationSettings)
	}
	m.applicationSetting(c.Part("rules"), uiText("保留规则"),
		uiText("同时检查两项上限，满足任一清理条件的旧版本会自动移除。填 0 表示不限。"), func() {
			ui.Row(c).Gap(6).Items(ui.Center).Children(func() {
				ui.Column(c.Part("duration")).Gap(2).Children(func() {
					ui.Text(c, uiText("保留天数")).FontSize(11).Muted()
					ui.TextField(c, &state.days, uiText("保留天数"), "0").Width(76).Height(32)
				})
				ui.Column(c.Part("count")).Gap(2).Children(func() {
					ui.Text(c, uiText("每文件版本数")).FontSize(11).Muted()
					ui.TextField(c, &state.versions, uiText("每文件版本数"), "0").Width(86).Height(32)
				})
				changed := state.days.Text() != strconv.Itoa(max(0, days)) || state.versions.Text() != strconv.Itoa(max(0, versions))
				if ui.Button(c.Part("apply"), uiText("应用")).Height(32).Disabled(!changed || state.action != nil).Clicked() {
					c.After(func() { m.applyHistoryRetention(store, state) })
				}
			})
		})
}

func (m *editorApp) applyHistoryRetention(store *localHistoryStore, state *localHistoryPage) {
	days, dayErr := strconv.Atoi(strings.TrimSpace(state.days.Text()))
	versions, versionErr := strconv.Atoi(strings.TrimSpace(state.versions.Text()))
	if dayErr != nil || days < 0 || days > maxHistoryDays {
		state.problem = fmt.Sprintf(uiText("保留天数请输入 0 到 %d 的整数"), maxHistoryDays)
		return
	}
	if versionErr != nil || versions < 0 || versions > maxHistoryCount {
		state.problem = fmt.Sprintf(uiText("每文件版本数请输入 0 到 %d 的整数"), maxHistoryCount)
		return
	}
	state.days.SetText(strconv.Itoa(days))
	state.versions.SetText(strconv.Itoa(versions))
	currentDays, currentCount := m.appSettings.draft.LocalHistory.limits()
	if days == max(0, currentDays) && versions == max(0, currentCount) {
		state.problem = ""
		return
	}
	if days == 0 {
		days = -1
	}
	if versions == 0 {
		versions = -1
	}
	m.appSettings.draft.LocalHistory.Days, m.appSettings.draft.LocalHistory.Versions = days, versions
	m.saveApplicationSettings()
	if m.appSettings.original.LocalHistory.Days != days || m.appSettings.original.LocalHistory.Versions != versions {
		return
	}
	state.notice, state.problem = "", ""
	state.action = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
		removed, err := store.clean(time.Now(), false)
		return fmt.Sprintf(uiText("已应用保留规则，清理 %d 个旧版本"), removed), err
	})
}

type localHistoryRow struct {
	file    *localHistoryFile
	version *localHistoryVersion
}

func (m *editorApp) historyRecords(c ui.C, state *localHistoryPage) {
	ui.TextField(c.Part("file-filter"), &state.filter, uiText("筛选历史文件"), uiText("筛选文件名或路径")).FillWidth().Height(32)
	filter := strings.ToLower(strings.TrimSpace(state.filter.Text()))
	var rows []localHistoryRow
	for i := range state.catalog.files {
		file := &state.catalog.files[i]
		if filter != "" && !strings.Contains(strings.ToLower(file.source), filter) {
			continue
		}
		rows = append(rows, localHistoryRow{file: file})
		if file.id == state.expanded {
			for i := range file.versions {
				rows = append(rows, localHistoryRow{file: file, version: &file.versions[i]})
			}
		}
	}
	if len(rows) == 0 {
		message := "还没有本地历史记录"
		if !state.loaded {
			message = "正在读取本地历史…"
		} else if filter != "" {
			message = "没有匹配的历史记录"
		}
		ui.Text(c, message).FontSize(13).Muted().PadY(10)
		return
	}
	ui.List(c.Part("list"), &state.list, len(rows), func(item ui.ListRow) {
		row := rows[item.Index]
		if row.version == nil {
			m.historyFileRow(item, state, row.file)
			return
		}
		m.historyVersionRow(item, state, row.file, *row.version)
	}).FillWidth().Height(248).Label(uiText("本地历史记录"))
}

func (m *editorApp) historyFileRow(item ui.ListRow, state *localHistoryPage, file *localHistoryFile) {
	item.Element.Height(52).PadX(8).Gap(8).Items(ui.Center).Radius(8).Background(m.theme.Card).Children(func() {
		c := item.C
		selected := 0
		for _, version := range file.versions {
			if _, ok := state.selected[version.key()]; ok {
				selected++
			}
		}
		checked := selected == len(file.versions)
		name, path := filepath.Base(file.source), file.source
		if file.source == "" {
			name, path = "未知文件", file.id
		}
		label := "选择 " + name + " 的所有版本"
		if ui.Checkbox(c.Part("select-file"), &checked, "").Label(label).Tooltip(label).Mixed(selected > 0 && !checked).
			Size(23, 28).Shrink(0).Disabled(state.action != nil).Changed() {
			c.After(func() {
				for _, version := range file.versions {
					state.selectVersion(version, checked)
				}
			})
		}
		expanded := state.expanded == file.id
		chevron := chevronRightIcon
		if expanded {
			chevron = chevronDownIcon
		}
		target := c.Part("expand")
		row := ui.Row(target).Grow(1).MinWidth(0).FillHeight().PadX(5).Gap(7).Items(ui.Center).Radius(7).
			HitWith(ui.ID(target), name, gfx.HitOptions{Role: gfx.RoleButton, Expandable: true, Expanded: expanded}).
			Tooltip(path).Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Border(m.theme.Ring))
		row.Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, name).FontSize(13).Ellipsis()
				ui.Text(c, path).FontSize(11).Muted().Ellipsis()
			})
			ui.Text(c, fmt.Sprintf(uiText("%d 个版本 · %s"), len(file.versions), historySize(file.bytes))).Muted().FontSize(12).Shrink(0)
			ui.Icon(c, chevron).Size(16, 16).Color(m.theme.MutedForeground)
		})
		if row.Clicked() {
			c.After(func() {
				if expanded {
					state.expanded = ""
				} else {
					state.expanded = file.id
				}
			})
		}
	})
}

func (m *editorApp) historyVersionRow(item ui.ListRow, state *localHistoryPage, file *localHistoryFile, version localHistoryVersion) {
	item.Element.Height(38).PadLeft(30).PadRight(8).Gap(9).Items(ui.Center).Children(func() {
		c := item.C
		_, checked := state.selected[version.key()]
		if ui.Checkbox(c.Part("select-version"), &checked, version.at.Local().Format(time.DateTime)).
			Grow(1).Height(28).Disabled(state.action != nil).Changed() {
			c.After(func() { state.selectVersion(version, checked) })
		}
		ui.Text(c, historySize(version.bytes)).Muted().FontSize(12).Shrink(0)
		if ui.Button(c.Part("show-version"), uiText("比对版本")).Ghost().Height(28).FontSize(12).Disabled(state.action != nil).Clicked() {
			c.After(func() { m.openHistoryComparison(file.source, version) })
		}
	})
}

func (state *localHistoryPage) selectVersion(version localHistoryVersion, selected bool) {
	if selected {
		if state.selected == nil {
			state.selected = make(map[string]localHistoryVersion)
		}
		state.selected[version.key()] = version
		return
	}
	delete(state.selected, version.key())
}

func (m *editorApp) historyCleanupDialog(c ui.C, store *localHistoryStore, state *localHistoryPage) {
	if !state.confirm {
		return
	}
	title, action := uiText("清理所选历史记录？"), "确认清理"
	count, bytes := len(state.selected), int64(0)
	for _, version := range state.selected {
		bytes += version.bytes
	}
	if state.clearAll {
		title, action = uiText("清空全部本地历史？"), "确认清空"
		count, bytes = state.catalog.count, state.catalog.bytes
	}
	ui.ModalWith(c, &state.confirm, title, ui.ModalSpec{Width: 450}, func() {
		ui.Wrap(c, fmt.Sprintf(uiText("将永久删除 %d 个历史版本，释放约 %s。"), count, historySize(bytes))).FillWidth()
	}, func() {
		if ui.Button(c.Part("cancel"), uiText("取消")).Ghost().Clicked() {
			state.confirm = false
		}
		if ui.Button(c.Part("confirm"), action).Clicked() {
			c.After(func() {
				all := state.clearAll
				versions := make([]localHistoryVersion, 0, len(state.selected))
				for _, version := range state.selected {
					versions = append(versions, version)
				}
				state.confirm, state.notice, state.problem = false, "", ""
				state.action = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
					var removed int
					var err error
					if all {
						removed, err = store.clean(time.Now(), true)
					} else {
						removed, err = store.remove(versions)
					}
					return fmt.Sprintf(uiText("已清理 %d 个历史版本"), removed), err
				})
			})
		}
	})
}

func historySize(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
