package editor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
)

const (
	updateSourceGitHub    = "github"
	updateSourceCNB       = "cnb"
	officialCNBRepository = "https://cnb.cool/yuands9/editorx"
)

var cnbRepositoryPath = regexp.MustCompile(`^/[A-Za-z0-9_][A-Za-z0-9_.-]*(?:/[A-Za-z0-9_][A-Za-z0-9_.-]*)+$`)

func normalizeCNBRepository(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	address, err := url.Parse(value)
	if err != nil || address.Scheme != "https" || !strings.EqualFold(address.Host, "cnb.cool") ||
		address.User != nil || address.RawQuery != "" || address.ForceQuery || address.Fragment != "" || address.RawPath != "" {
		return "", errors.New(uiText("请输入公开的 CNB 仓库地址，例如 https://cnb.cool/组织/仓库。"))
	}
	path := strings.TrimSuffix(strings.TrimSuffix(address.Path, "/"), ".git")
	if !cnbRepositoryPath.MatchString(path) {
		return "", errors.New(uiText("请输入公开的 CNB 仓库地址，例如 https://cnb.cool/组织/仓库。"))
	}
	return "https://cnb.cool" + path, nil
}

func cnbReleaseAsset(repository string, release editorRelease) string {
	if repository == "" || release.Download == "" || release.PageOnly {
		return ""
	}
	tag, valid := strings.CutPrefix(release.Page, editorRepositoryURL+"/releases/tag/")
	if _, err := editorVersionParts(tag); !valid || err != nil {
		return ""
	}
	return repository + "/-/releases/download/" + tag + "/" + updateExecutable
}

var editorCNBHTTP = func() *http.Client {
	client := *editorUpdateHTTP
	client.CheckRedirect = func(request *http.Request, previous []*http.Request) error {
		if len(previous) >= 6 || request.URL.Scheme != "https" || request.URL.User != nil {
			return errors.New(uiText("更新链接跳转无效"))
		}
		switch request.URL.Host {
		case "cnb.cool", "asset.cnb.cool":
			return nil
		default:
			return errors.New(uiText("更新链接跳转到了未知地址"))
		}
	}
	return &client
}()

func downloadPreferredEditorUpdate(ctx context.Context, official, mirror *http.Client, repository string, release editorRelease, path string, progress *dictionaryProgress) error {
	if address := cnbReleaseAsset(repository, release); address != "" {
		mirrored := release
		mirrored.Download = address
		attempt, cancel := context.WithTimeout(ctx, time.Minute)
		err := receiveEditorUpdate(attempt, mirror, mirrored, path, progress)
		cancel()
		if err == nil {
			return nil
		}
		progress.received.Store(0)
	}
	return downloadEditorUpdate(ctx, official, release, path, progress)
}

func (preferences applicationPreferences) updateSource() string {
	if preferences.UpdateSource == updateSourceCNB ||
		preferences.UpdateSource == "" && preferences.CNBRepository != "" {
		return updateSourceCNB
	}
	return updateSourceGitHub
}

func (preferences applicationPreferences) updateRepository() string {
	if preferences.updateSource() != updateSourceCNB {
		return ""
	}
	if preferences.CNBRepository != "" {
		return preferences.CNBRepository
	}
	return officialCNBRepository
}

func (m *editorApp) updateMirrorSettings(c ui.C) {
	settings := &m.appSettings
	if settings.mirrorSaved != settings.draft.CNBRepository {
		settings.mirrorSaved = settings.draft.CNBRepository
		settings.mirrorField.SetText(settings.mirrorSaved)
	}
	selected := settings.draft.updateSource()
	m.applicationSetting(c, uiText("更新下载源"),
		uiText("选择更新包的下载来源。CNB 下载或校验失败时回退到 GitHub。"), func() {
			previous := selected
			ui.Select(c.Part("source"), &selected, []string{updateSourceGitHub, updateSourceCNB}, func(source string) string {
				if source == updateSourceCNB {
					return "CNB"
				}
				return uiText("GitHub（默认）")
			}).Label(uiText("更新下载源")).Width(150).Height(32)
			if selected != previous {
				c.After(func() { m.saveUpdateSource(selected) })
			}
		})
	if settings.draft.updateSource() != updateSourceCNB {
		return
	}
	repository := settings.draft.updateRepository()
	if !settings.mirrorEdit {
		m.applicationSetting(c.Part("cnb-repository"), uiText("CNB 镜像仓库"), repository, func() {
			if ui.Button(c, uiText("更改镜像地址")).Ghost().Height(30).Clicked() {
				c.After(func() {
					settings.mirrorField.SetText(settings.mirrorSaved)
					settings.mirrorEdit = true
				})
			}
		})
		return
	}
	ui.Row(c.Part("mirror")).FillWidth().PadX(12).PadY(8).Gap(8).Items(ui.Center).
		Radius(10).Border(1, m.theme.Border).Children(func() {
		label := uiText("CNB 镜像仓库")
		ui.Text(c, label).Muted().FontSize(12)
		ui.TextFieldBase(c.Part("address"), &settings.mirrorField, label, officialCNBRepository).
			Grow(1).MinWidth(0).Height(32)
		if ui.Button(c.Part("save"), uiText("保存下载源")).Outline().Height(32).
			Disabled(strings.TrimSpace(settings.mirrorField.Text()) == settings.mirrorSaved).Clicked() {
			c.After(m.saveUpdateMirror)
		}
		if ui.Button(c.Part("cancel"), uiText("取消")).Ghost().Height(32).Clicked() {
			c.After(func() { settings.mirrorEdit, settings.problem = false, "" })
		}
	})
}

func (m *editorApp) saveUpdateSource(source string) {
	preferences, err := m.applicationPreferences()
	if err != nil {
		m.appSettings.problem = uiText("无法读取设置：") + err.Error()
		return
	}
	preferences.UpdateSource = source
	if m.persistApplicationSettings(preferences) {
		m.appSettings.mirrorEdit = false
	}
}

func (m *editorApp) saveUpdateMirror() {
	settings := &m.appSettings
	repository, err := normalizeCNBRepository(settings.mirrorField.Text())
	if err != nil {
		settings.problem = err.Error()
		return
	}
	preferences, err := m.applicationPreferences()
	if err != nil {
		settings.problem = uiText("无法读取设置：") + err.Error()
		return
	}
	preferences.CNBRepository = repository
	if m.persistApplicationSettings(preferences) {
		settings.mirrorSaved = repository
		settings.mirrorField.SetText(repository)
		settings.mirrorEdit = false
	}
}
