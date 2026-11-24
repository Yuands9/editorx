package editor

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

func (m *editorApp) refreshTranslationDictionary() {
	state := m.translation
	state.dictionaryChecked, state.dictionaryBroken = true, false
	path, err := m.textAssetPath("", dictionaryDirectory)
	if err != nil {
		state.dictionary, state.dictionaryProblem = nil, err.Error()
		return
	}
	info, err := readDictionaryInfo(path)
	state.dictionary, state.dictionaryProblem = info, ""
	if err != nil {
		state.dictionaryBroken = true
		state.dictionaryProblem = err.Error()
	}
}

func (m *editorApp) startDictionaryDownload() {
	state := m.translation
	if state == nil || !m.translationEnabled || state.download != nil {
		return
	}
	m.refreshTranslationDictionary()
	if state.dictionary != nil || state.dictionaryProblem != "" {
		return
	}
	state.dictionaryProblem, state.message = "", ""
	progress := &dictionaryProgress{invalidate: m.invalidator.Invalidate}
	destination := filepath.Join(m.dataDirectory, dictionaryDirectory)
	state.downloadProgress = progress
	state.download = ui.NewEffect[dictionaryInfo, int](func(ctx context.Context) (dictionaryInfo, error) {
		return installTranslationDictionary(ctx, editorUpdateHTTP, englishChineseDictionary, destination, progress)
	})
}

func (m *editorApp) updateTranslationDictionary(c ui.C) {
	state := m.translation
	if state == nil || state.download == nil {
		return
	}
	ui.Effect(c, state.download, 0)
	info, err, ready := state.download.Take()
	if !ready {
		return
	}
	state.download, state.downloadProgress = nil, nil
	if err != nil {
		state.dictionaryProblem = "下载失败：" + err.Error() + "。请检查网络或代理后重试。"
		return
	}
	state.dictionary, state.dictionaryProblem = &info, ""
	if state.mode == translationLocalMode && state.localLookup && !state.hasOutput && !state.managing && state.draft == nil && state.matchesQuery() {
		m.lookupTypedTranslation()
	}
}

func (m *editorApp) dictionaryDownloadControls(c ui.C, state *translationPanelState, blocked bool) {
	if state.download != nil {
		total := state.downloadProgress.total.Load()
		if total <= 0 {
			total = englishChineseDictionary.size
		}
		received := state.downloadProgress.received.Load()
		fraction := min(1, float32(received)/float32(total))
		ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("下载中 · %.1f MiB", float64(received)/(1<<20))).Grow(1).Muted().FontSize(12)
			if ui.Button(c, "取消下载").Ghost().Height(26).FontSize(12).Disabled(blocked).Clicked() {
				c.After(func() { state.download, state.downloadProgress = nil, nil })
			}
		})
		ui.Progress(c, fraction).FillWidth().Height(5)
	} else if state.dictionary == nil && !state.dictionaryBroken {
		ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			if ui.Button(c, "下载英汉词库").Icon(downloadIcon).Ghost().Height(28).FontSize(12).Tooltip("下载约 21.7 MiB").
				Disabled(blocked || state.translating()).Clicked() {
				c.After(m.startDictionaryDownload)
			}
			if state.managing || !state.hasOutput || !state.matchesQuery() {
				ui.Text(c, "约 21.7 MiB").Muted().FontSize(12)
			}
		})
	}
	if state.dictionaryProblem != "" && (state.managing || !state.hasOutput || !state.matchesQuery()) {
		ui.Wrap(c, state.dictionaryProblem).FillWidth().FontSize(12).Color(m.theme.Danger)
	}
}

func (m *editorApp) dictionaryCard(c ui.C, state *translationPanelState, blocked bool) {
	m.assetCard(c).Children(func() {
		ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
			ui.Text(c, "英汉词库").Grow(1).Bold().FontSize(14)
			if state.dictionary != nil {
				ui.Text(c, "已下载").Muted().FontSize(12)
			}
		})
		if state.removeDictionary {
			ui.Wrap(c, "移除英汉词库，保留个人和项目译文。").FillWidth().Muted().FontSize(12)
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, "确认移除").Outline().FontSize(12).Disabled(blocked).Clicked() {
					c.After(func() {
						if err := removeTranslationDictionary(filepath.Join(m.dataDirectory, dictionaryDirectory)); err != nil {
							state.dictionaryProblem = err.Error()
							return
						}
						state.removeDictionary = false
						m.refreshTranslationDictionary()
					})
				}
				if ui.Button(c, "保留词库").Ghost().FontSize(12).Disabled(blocked).Clicked() {
					state.removeDictionary = false
				}
			})
		} else if (state.dictionary != nil || state.dictionaryBroken) && ui.Button(c, "移除下载词库…").Ghost().FontSize(12).Disabled(blocked).Clicked() {
			state.removeDictionary = true
		}
		m.dictionaryDownloadControls(c.Part("download"), state, blocked)
		if ui.Button(c, "ECDICT 来源与许可 ↗").Ghost().FontSize(11).Disabled(blocked).Clicked() {
			c.After(func() {
				if err := app.OpenURL("https://github.com/skywind3000/ECDICT"); err != nil {
					state.dictionaryProblem = err.Error()
				}
			})
		}
	})
}
