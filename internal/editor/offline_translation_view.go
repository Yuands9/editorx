package editor

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/vivaui/viva/ui"
)

type offlineComponentStatus struct {
	installed *offlineInstallation
	err       error
}

type offlineComponentDownload struct {
	model    offlineComponent
	bytes    int64
	progress *dictionaryProgress
	task     *ui.EffectState[bool, int]
}

type offlineTranslationState struct {
	runtime  offlineComponentStatus
	models   map[string]offlineComponentStatus
	problem  string
	request  *ui.EffectState[offlineTranslationResult, int]
	download *offlineComponentDownload
	remove   bool
}

func (m *editorApp) translationOfflineState(state *translationPanelState) *offlineTranslationState {
	if state.offline == nil {
		state.offline = &offlineTranslationState{models: make(map[string]offlineComponentStatus, len(offlineModels))}
		m.refreshOfflineComponents(state.offline)
	}
	return state.offline
}

func (m *editorApp) refreshOfflineComponents(offline *offlineTranslationState) {
	offline.runtime.installed, offline.runtime.err = readOfflineInstallation(m.dataDirectory, offlineRuntime)
	for _, model := range offlineModels {
		installed, err := readOfflineInstallation(m.dataDirectory, model)
		offline.models[model.ID] = offlineComponentStatus{installed: installed, err: err}
	}
}

func (offline *offlineTranslationState) downloadSize(model offlineComponent) (int64, error) {
	status := offline.models[model.ID]
	if offline.runtime.err != nil {
		return 0, offline.runtime.err
	}
	if status.err != nil {
		return 0, status.err
	}
	var missing int64
	if offline.runtime.installed == nil {
		missing += offlineRuntime.downloadBytes()
	}
	if status.installed == nil {
		missing += model.downloadBytes()
	}
	return missing, nil
}

func (m *editorApp) offlineComponentsCard(c ui.C, state *translationPanelState, blocked bool) {
	offline := m.translationOfflineState(state)
	m.assetCard(c).Children(func() {
		ui.Text(c, uiText("离线整句")).Bold().FontSize(14)
		hasFiles := offline.runtime.installed != nil || offline.runtime.err != nil
		for _, model := range offlineModels {
			status := offline.models[model.ID]
			hasFiles = hasFiles || status.installed != nil || status.err != nil
			ui.Row(c.Key(model.ID)).FillWidth().Gap(6).Items(ui.Center).Children(func() {
				ui.Wrap(c, model.Label).Grow(1).MinWidth(0).FontSize(12)
				bytes, err := offline.downloadSize(model)
				switch {
				case err != nil:
					ui.Text(c, uiText("需重装")).Muted().FontSize(12)
				case bytes == 0:
					ui.Text(c, uiText("已下载")).Muted().FontSize(12)
				default:
					if ui.Button(c, fmt.Sprintf(uiText("下载 %.1f MiB"), float64(bytes)/(1<<20))).Outline().Height(28).FontSize(12).
						Tooltip(uiText("下载 ") + model.Label).Disabled(blocked || offline.download != nil || offline.remove).Clicked() {
						c.After(func() { m.startOfflineDownload(model) })
					}
				}
			})
		}
		if offline.download != nil {
			m.offlineDownloadProgress(c.Part("progress"), offline, blocked)
		} else if offline.remove {
			ui.Wrap(c, uiText("移除两个语言方向的组件。")).FillWidth().Muted().FontSize(12)
			ui.Row(c).Gap(6).Children(func() {
				if ui.Button(c, uiText("确认移除离线组件")).Outline().FontSize(12).Disabled(blocked).Clicked() {
					c.After(func() {
						err := removeOfflineComponents(m.dataDirectory)
						m.refreshOfflineComponents(offline)
						offline.remove, offline.problem = false, ""
						if err != nil {
							offline.problem = err.Error()
						}
					})
				}
				if ui.Button(c, uiText("取消移除")).Ghost().FontSize(12).Disabled(blocked).Clicked() {
					offline.remove = false
				}
			})
		} else if hasFiles && ui.Button(c, uiText("移除离线组件…")).Ghost().FontSize(12).Disabled(blocked).Clicked() {
			offline.remove = true
		}
		if offline.problem != "" {
			ui.Wrap(c, offline.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
		}
	})
}

func (m *editorApp) offlineDownloadProgress(c ui.C, offline *offlineTranslationState, blocked bool) {
	download := offline.download
	fraction := min(1, float32(download.progress.received.Load())/float32(download.bytes))
	label := fmt.Sprintf(uiText("下载中 · %.0f%%"), fraction*100)
	if fraction >= 1 {
		label = "正在安装…"
	}
	ui.Row(c).FillWidth().Gap(6).Items(ui.Center).Children(func() {
		ui.Text(c, label).Grow(1).Muted().FontSize(12)
		if ui.Button(c, uiText("取消组件下载")).Ghost().Height(26).FontSize(12).Disabled(blocked).Clicked() {
			c.After(func() {
				offline.download = nil
				m.refreshOfflineComponents(offline)
			})
		}
	})
	ui.Progress(c, fraction).FillWidth().Height(5)
}

func (m *editorApp) startOfflineDownload(model offlineComponent) {
	state := m.translation
	if state == nil || state.translating() {
		return
	}
	offline := m.translationOfflineState(state)
	if offline.download != nil {
		return
	}
	m.refreshOfflineComponents(offline)
	bytes, err := offline.downloadSize(model)
	if err != nil || bytes == 0 {
		return
	}
	directory := m.dataDirectory
	runtime := offlineRuntime
	progress := &dictionaryProgress{invalidate: m.invalidator.Invalidate}
	task := ui.NewEffect[bool, int](func(ctx context.Context) (bool, error) {
		client := *editorUpdateHTTP
		client.Timeout = 15 * time.Minute
		client.CheckRedirect = func(request *http.Request, previous []*http.Request) error {
			if request.URL.Scheme != "https" || len(previous) >= 6 {
				return fmt.Errorf("%s", uiText("离线组件下载跳转无效"))
			}
			return nil
		}
		if _, err := installOfflineComponent(ctx, &client, directory, runtime, progress); err != nil {
			return false, err
		}
		_, err := installOfflineComponent(ctx, &client, directory, model, progress)
		return err == nil, err
	})
	offline.download = &offlineComponentDownload{model: model, bytes: bytes, progress: progress, task: task}
	offline.problem = ""
}

func (m *editorApp) startOfflineTranslation(state *translationPanelState) {
	source, target := state.querySource, state.queryTarget
	model := offlineLanguageModel(source, target)
	if model == nil {
		return
	}
	offline := m.translationOfflineState(state)
	m.refreshOfflineComponents(offline)
	if bytes, err := offline.downloadSize(*model); err != nil || bytes != 0 {
		return
	}
	directory, query := m.dataDirectory, state.target.selected
	offline.request = ui.NewEffect[offlineTranslationResult, int](func(ctx context.Context) (offlineTranslationResult, error) {
		return translateOffline(ctx, directory, source, target, query)
	})
}

func (m *editorApp) updateOfflineTranslation(c ui.C) {
	state := m.translation
	if state == nil || state.offline == nil {
		return
	}
	offline := state.offline
	if download := offline.download; download != nil {
		ui.Effect(c.Part("download"), download.task, 0)
		if _, err, ready := download.task.Take(); ready {
			offline.download = nil
			m.refreshOfflineComponents(offline)
			if err != nil {
				offline.problem = download.model.Label + uiText("下载失败：") + err.Error()
			}
		}
	}
	if offline.request == nil {
		return
	}
	ui.Effect(c.Part("translate"), offline.request, 0)
	result, err, ready := offline.request.Take()
	if !ready {
		return
	}
	offline.request = nil
	if err != nil {
		state.problem = uiText("离线翻译失败：") + err.Error()
		return
	}
	if err := state.validateTarget(m.active); err != nil || !state.matchesLanguages() {
		state.problem = uiText("原文或语言已变化，请重新翻译")
		return
	}
	state.output.SetText(result.Text)
	state.hasOutput = true
}
