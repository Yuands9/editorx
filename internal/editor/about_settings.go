package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type editorUpdateState struct {
	open     bool
	release  editorRelease
	check    *ui.EffectState[editorRelease, int]
	download *ui.EffectState[struct{}, int]
	staged   string
	problem  string
}

func (m *editorApp) aboutSettings(c ui.C) {
	state := &m.update
	ui.Row(c.Part("editorx")).FillWidth().Pad(16).Gap(16).Items(ui.Center).
		Background(m.theme.Card).Radius(14).Children(func() {
		ui.Image(c, editorLogo(m.light)).Size(56, 56).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, applicationName).FontSize(23).Bold()
			ui.Wrap(c, "一个 Windows 文本编辑器，用来打开、查看和编辑日常文件。").
				FillWidth().FontSize(13).Muted()
		})
	})
	build := "Windows x64 · 便携版"
	if hasWordEditor {
		build = "Windows x64 · 本机构建"
	}
	m.applicationSetting(c.Part("version"), "版本 "+version.Number, build, func() {
		label := "检查更新"
		if state.staged != "" && state.download == nil {
			label = "查看已下载的更新"
		}
		if ui.Button(c, label).Icon(refreshIcon).Height(32).Clicked() {
			c.After(m.checkEditorUpdate)
		}
	})
	m.applicationSetting(c.Part("project"), "开源项目", "Apache-2.0", func() {
		if ui.Button(c, "打开 GitHub").Outline().Height(32).Clicked() {
			c.After(func() {
				if err := app.OpenURL(editorRepositoryURL); err != nil {
					m.appSettings.problem = "无法打开 GitHub：" + err.Error()
				}
			})
		}
	})
}

func (m *editorApp) checkEditorUpdate() {
	state := &m.update
	state.open = true
	if state.check != nil || state.download != nil || state.staged != "" {
		return
	}
	state.release, state.problem = editorRelease{}, ""
	state.check = ui.NewEffect[editorRelease, int](func(ctx context.Context) (editorRelease, error) {
		ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
		defer cancel()
		return fetchEditorRelease(ctx, editorUpdateHTTP, latestReleaseAPI, version.Number)
	})
}

func (m *editorApp) downloadEditorRelease() {
	state := &m.update
	if state.download != nil || state.staged != "" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		state.problem = "无法定位当前程序：" + err.Error()
		return
	}
	file, err := os.CreateTemp(filepath.Dir(executable), ".editorx-update-*.exe")
	if err != nil {
		state.problem = "无法在程序目录创建更新文件：" + err.Error()
		return
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(path)
		state.problem = "无法准备下载文件：" + err.Error()
		return
	}
	release := state.release
	state.staged, state.problem = path, ""
	state.download = ui.NewEffect[struct{}, int](func(ctx context.Context) (struct{}, error) {
		return struct{}{}, downloadEditorUpdate(ctx, editorUpdateHTTP, release, path)
	})
}

func (m *editorApp) updateProgress(c ui.C) {
	state := &m.update
	if state.check != nil {
		ui.Effect(c.Part("check"), state.check, 0)
		if release, err, ready := state.check.Take(); ready {
			state.check = nil
			if err != nil {
				state.problem = "检查更新失败：" + err.Error()
			} else {
				state.release = release
			}
		}
	}
	if state.download != nil {
		ui.Effect(c.Part("download"), state.download, 0)
		if _, err, ready := state.download.Take(); ready {
			state.download = nil
			if err != nil {
				state.staged = ""
				state.problem = "下载更新失败：" + err.Error()
			}
		}
	}
}

func (m *editorApp) updateDialog(c ui.C) {
	state := &m.update
	if !state.open || !m.appSettings.open {
		return
	}
	release := state.release
	title := "检查更新"
	if release.comparison > 0 {
		title = "发现新版本"
	}
	ui.ModalWith(c, &state.open, title, ui.ModalSpec{Width: 540}, func() {
		switch {
		case state.check != nil:
			ui.Text(c, "正在查询 GitHub 发布版本…").Muted()
		case release.comparison > 0:
			ui.Text(c, "EditorX "+release.version).FontSize(19).Bold()
			ui.Text(c, fmt.Sprintf("当前版本 %s · %.1f MB", version.Number, float64(release.size)/(1<<20))).Muted().FontSize(12)
			if release.notes != "" {
				ui.Slot(c).Height(8)
				if utf8.RuneCountInString(release.notes) > 160 || strings.Count(release.notes, "\n") > 3 {
					ui.Scroll(c.Part("release-notes")).FillWidth().Height(144).Scrollbars().Children(func() {
						ui.Wrap(c, release.notes).FillWidth().FontSize(13)
					})
				} else {
					ui.Wrap(c, release.notes).FillWidth().FontSize(13)
				}
			}
			if state.download != nil {
				ui.Text(c, "正在下载并校验更新…").Muted().FontSize(12)
			} else if state.staged != "" {
				ui.Text(c, "更新已下载并校验，可保存会话后重启。").FontSize(12).Color(m.theme.Success)
			}
		case state.problem == "" && release.comparison < 0:
			ui.Wrap(c, "当前版本 "+version.Number+"。最新公开版本为 "+release.version+"。").FillWidth()
		case state.problem == "":
			ui.Text(c, "已是最新版本 "+version.Number)
		}
		if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().Color(m.theme.Danger).FontSize(13)
		}
	}, func() {
		dismiss := "关闭"
		if release.comparison > 0 {
			dismiss = "稍后"
		}
		if ui.Button(c, dismiss).Ghost().Clicked() {
			state.open = false
		}
		if release.version != "" && ui.Button(c, "发布说明").Outline().Clicked() {
			c.After(func() {
				if err := app.OpenURL(release.page); err != nil {
					state.problem = "无法打开发布说明：" + err.Error()
				}
			})
		}
		if release.comparison <= 0 || state.download != nil {
			return
		}
		switch {
		case hasWordEditor:
			if ui.Button(c, "下载公开版").Clicked() {
				c.After(func() {
					if err := app.OpenURL(release.download); err != nil {
						state.problem = "无法打开下载链接：" + err.Error()
					}
				})
			}
		case state.staged != "":
			if ui.Button(c, "更新并重启").Disabled(m.desktop == nil).Clicked() {
				c.After(func() { m.desktop.restartForUpdate(state.staged) })
			}
		default:
			if ui.Button(c, "下载更新").Clicked() {
				c.After(m.downloadEditorRelease)
			}
		}
	})
}
