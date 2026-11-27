package editor

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

func (m *editorApp) aboutSettings(c ui.C) {
	state := m.editorUpdates()
	buildType := uiText("便携版")
	if hasWordEditor {
		buildType = uiText("本机构建")
	}
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86-64"
	case "arm64":
		arch = "ARM64"
	}
	fields := []struct {
		label, value string
		icon         gfx.Icon
	}{
		{"版本", "v" + version.Number + " · " + buildType, infoIcon},
		{"运行平台", "Windows · " + arch, browserIcon},
		{"构建", version.Build(), settingsIcon},
		{"运行时", runtime.Version(), codeIcon},
	}
	ui.Row(c.Part("editorx")).FillWidth().Pad(16).Gap(16).Items(ui.Center).
		Background(m.theme.Card).Radius(14).Border(1, m.theme.Border).Children(func() {
		ui.Image(c, editorLogo(m.light)).Size(56, 56).Shrink(0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, applicationName).FontSize(23).Bold()
			ui.Wrap(c, uiText("一个 Windows 文本编辑器，用来打开、查看和编辑日常文件。")).FillWidth().FontSize(13).Muted()
		})
		label := "复制版本信息"
		if m.appSettings.aboutCopied {
			label = "已复制"
		}
		if ui.Button(c.Part("copy-version"), uiText(label)).Icon(copyIcon).Outline().Height(32).Shrink(0).Clicked() {
			c.After(func() {
				var report strings.Builder
				report.WriteString(applicationName)
				for _, field := range fields {
					fmt.Fprintf(&report, "\n%s %s", uiText(field.label), field.value)
				}
				if err := app.SetClipboardText(report.String()); err != nil {
					m.appSettings.problem = uiText("无法复制版本信息：") + err.Error()
					m.appSettings.aboutCopied = false
					return
				}
				m.appSettings.problem = ""
				m.appSettings.aboutCopied = true
			})
		}
	})
	ui.Text(c, uiText("版本信息")).Bold().FontSize(14)
	ui.Column(c.Part("version-info")).FillWidth().Pad(12).Gap(8).
		Background(m.theme.Card).Radius(12).Border(1, m.theme.Border).Children(func() {
		for row := 0; row < len(fields); row += 2 {
			ui.Row(c.KeyInt(row)).FillWidth().Gap(8).Children(func() {
				for _, field := range fields[row : row+2] {
					ui.Column(c.Key(field.label)).Grow(1).Basis(0).MinWidth(0).Pad(10).Gap(3).
						Background(m.theme.Background).Radius(8).Children(func() {
						ui.Row(c).Gap(5).Items(ui.Center).Children(func() {
							ui.Icon(c, field.icon).Size(14, 14).Color(m.theme.MutedForeground)
							ui.Text(c, uiText(field.label)).FontSize(12).Muted()
						})
						ui.Wrap(c, field.value).FillWidth().FontSize(13)
					})
				}
			})
		}
	})
	m.applicationSetting(c.Part("update"), uiText("软件更新"), uiText("检查 GitHub 上的最新公开版本。"), func() {
		label := "检查更新"
		if state.staged != "" && state.download == nil {
			label = "查看已下载的更新"
		}
		if ui.Button(c, uiText(label)).Icon(refreshIcon).Height(32).Clicked() {
			c.After(m.checkEditorUpdate)
		}
	})
	enabled := !m.appSettings.draft.DisableAutoUpdate
	schedule := fmt.Sprintf(uiText("每次启动约 %g 秒后检查，运行期间每 %g 小时检查一次。"),
		editorUpdateStartupDelay.Seconds(), editorUpdateInterval.Hours())
	if m.applicationSettingSwitch(c.Part("auto-update"), uiText("自动检查更新"), schedule, &enabled) {
		m.appSettings.draft.DisableAutoUpdate = !enabled
		c.After(m.saveApplicationSettings)
	}
	m.applicationSetting(c.Part("project"), uiText("开源项目"), "Apache-2.0", func() {
		if ui.Button(c, uiText("打开 GitHub")).Outline().Height(32).Clicked() {
			c.After(func() {
				if err := app.OpenURL(editorRepositoryURL); err != nil {
					m.appSettings.problem = uiText("无法打开 GitHub：") + err.Error()
				}
			})
		}
	})
}

func updateButtonRecipe(theme gfx.Theme, variant components.ButtonVariant, state gfx.State) components.ButtonLook {
	look := components.ButtonRecipe(theme, variant, state)
	look.Foreground = theme.Success
	return look
}

func (m *editorApp) updateButton(c ui.C, blocked bool) {
	state := m.editorUpdates()
	if !state.available() {
		return
	}
	label := uiText("发现新版本 ") + state.cache.Release.Version
	if state.download != nil {
		label = uiText("正在下载更新")
	} else if state.staged != "" {
		label = uiText("更新已下载，安装并重启")
	}
	if ui.Button(c, label).Icon(updateAvailableIcon).IconOnly().Ghost().Size(32, 34).
		ButtonRecipe(updateButtonRecipe).Tooltip(label).Disabled(blocked).Clicked() {
		m.update.open = true
	}
}

func (m *editorApp) updateDialog(c ui.C) {
	if !m.update.open {
		return
	}
	state := m.editorUpdates()
	release := state.cache.Release
	title := uiText("检查更新")
	if release.comparison > 0 {
		title = "EditorX " + release.Version + uiText(" 可更新")
	}
	ui.ModalWith(c, &m.update.open, title, ui.ModalSpec{Width: 600}, func() {
		ui.Column(c).FillWidth().Gap(10).Children(func() {
			switch {
			case state.check != nil:
				ui.Text(c, uiText("正在检查新版本…")).Muted()
			case release.comparison > 0:
				description := fmt.Sprintf(uiText("当前版本 %s · 下载 %.1f MiB"), version.Number, float64(release.Size)/(1<<20))
				if release.PageOnly {
					description = uiText("已从 GitHub 公开发布页确认新版本。可打开发布页下载。")
				}
				if state.staged != "" && state.download == nil {
					description = uiText("当前版本 ") + version.Number + uiText("。安装前保存会话，完成后自动重新打开。")
				}
				ui.Wrap(c, description).FillWidth().Muted().FontSize(12)
				if release.Notes != "" {
					m.update.notes.Source = release.Notes
					m.update.notes.Style = uimarkdown.Style{Size: 13, LineHeight: 21}
					ui.Scroll(c.Part("release-notes")).FillWidth().Height(220).Scrollbars().Pad(12).
						Border(1, m.theme.Border).Radius(8).Children(func() {
						uimarkdown.View(c, uiText("更新内容"), &m.update.notes).FillWidth().Height(max(180, m.update.notes.Height()))
					})
				}
				if state.download != nil {
					received := state.progress.received.Load()
					ui.Text(c, fmt.Sprintf(uiText("正在下载并校验 · %.1f / %.1f MiB"), float64(received)/(1<<20), float64(release.Size)/(1<<20))).Muted().FontSize(12)
					ui.Progress(c, float32(received)/float32(release.Size)).FillWidth().Height(5)
				} else if state.staged != "" {
					ui.Text(c, uiText("更新已下载并校验，可以重启安装。")).FontSize(12).Color(m.theme.Success)
				}
			case state.problem == "" && release.comparison < 0:
				ui.Wrap(c, uiText("当前版本 ")+version.Number+uiText("。最新公开版本为 ")+release.Version+"。").FillWidth()
			case state.problem == "":
				ui.Text(c, uiText("已是最新版本 ")+version.Number)
			}
			if state.problem != "" {
				ui.Wrap(c, state.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
			}
		})
	}, func() {
		if release.comparison > 0 && state.check == nil {
			label := "跳过此版本"
			if state.download != nil {
				label = "取消下载"
			}
			if ui.Button(c, uiText(label)).Ghost().FontSize(12).Clicked() {
				if state.download != nil {
					c.After(m.cancelEditorDownload)
				} else {
					c.After(m.skipEditorUpdate)
				}
			}
			ui.Slot(c).Grow(1)
		}
		dismiss := "关闭"
		if release.comparison > 0 {
			dismiss = "稍后"
		}
		if state.download != nil {
			dismiss = "后台下载"
		}
		if ui.Button(c, uiText(dismiss)).Ghost().FontSize(12).Clicked() {
			m.update.open = false
		}
		if state.check != nil || state.download != nil {
			return
		}
		if release.comparison <= 0 {
			if state.problem != "" && ui.Button(c, uiText("重新检查")).FontSize(12).Clicked() {
				c.After(m.checkEditorUpdate)
			}
			return
		}
		switch {
		case release.PageOnly:
			if ui.Button(c, uiText("打开 GitHub 发布页")).FontSize(12).Clicked() {
				c.After(func() {
					if err := app.OpenURL(release.Page); err != nil {
						state.problem = uiText("无法打开发布页：") + err.Error()
					}
				})
			}
		case hasWordEditor:
			if ui.Button(c, uiText("下载公开版")).FontSize(12).Clicked() {
				c.After(func() {
					if err := app.OpenURL(release.Download); err != nil {
						state.problem = uiText("无法打开下载链接：") + err.Error()
					}
				})
			}
		case state.staged != "":
			if ui.Button(c, uiText("安装并重启")).FontSize(12).Disabled(m.desktop == nil).Clicked() {
				c.After(func() { m.desktop.restartForUpdate(state.staged) })
			}
		default:
			label := "下载更新"
			if state.problem != "" {
				label = "重试下载"
			}
			if ui.Button(c, uiText(label)).FontSize(12).Clicked() {
				c.After(m.downloadEditorRelease)
			}
		}
	})
}
