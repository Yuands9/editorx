package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Yuands9/editorx/internal/version"
	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	uimarkdown "github.com/vivaui/viva/ui/markdown"
)

const editorReleasesAPI = "https://api.github.com/repos/Yuands9/editorx/releases"
const editorReleasesPage = editorRepositoryURL + "/releases"
const editorReleasePageSize = 5

type editorReleasedVersion struct {
	tag, page, notes string
	published        time.Time
	prerelease       bool
}

type editorReleaseHistoryPage struct {
	versions []editorReleasedVersion
	more     bool
}

type editorReleaseHistory struct {
	entries     []editorReleasedVersion
	nextPage    int
	loadingPage int
	loading     *ui.EffectState[editorReleaseHistoryPage, int]
	problem     string
	expanded    string
	notes       uimarkdown.State
}

func fetchEditorReleaseHistory(ctx context.Context, client *http.Client, endpoint string, page int) (editorReleaseHistoryPage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s?per_page=%d&page=%d", endpoint, editorReleasePageSize, page), nil)
	if err != nil {
		return editorReleaseHistoryPage{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	request.Header.Set("User-Agent", applicationName+"/"+version.Number)
	response, err := client.Do(request)
	if err != nil {
		return editorReleaseHistoryPage{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return editorReleaseHistoryPage{}, &githubUpdateResponseError{status: response.StatusCode}
	}
	var records []githubEditorRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&records); err != nil {
		return editorReleaseHistoryPage{}, fmt.Errorf(uiText("无法读取发布信息：%w"), err)
	}
	result := editorReleaseHistoryPage{}
	for _, link := range response.Header.Values("Link") {
		if strings.Contains(link, `rel="next"`) {
			result.more = true
		}
	}
	for _, record := range records {
		if record.Draft || record.Tag == "" || record.Tag == "." || record.Tag == ".." {
			continue
		}
		result.versions = append(result.versions, editorReleasedVersion{
			tag: record.Tag, published: record.Published, prerelease: record.Prerelease, notes: editorReleaseNotes(record.Body),
			page: editorReleasesPage + "/tag/" + url.PathEscape(record.Tag),
		})
	}
	return result, nil
}

func (history *editorReleaseHistory) load(page int) {
	history.problem, history.loadingPage = "", page
	client := editorUpdateHTTP
	history.loading = ui.NewEffect[editorReleaseHistoryPage, int](func(ctx context.Context) (editorReleaseHistoryPage, error) {
		ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
		defer cancel()
		return fetchEditorReleaseHistory(ctx, client, editorReleasesAPI, page)
	})
}

func (m *editorApp) releaseHistorySettings(c ui.C) {
	history := &m.appSettings.releases
	if history.loadingPage == 0 {
		history.load(1)
	}
	if history.loading != nil {
		ui.Effect(c.Part("fetch"), history.loading, 0)
		if page, err, ready := history.loading.Take(); ready {
			history.loading = nil
			m.debug.record("update.history", err)
			if err != nil {
				history.problem = fmt.Sprintf(uiText("无法读取版本历史：%v"), err)
			} else {
				if history.loadingPage == 1 {
					history.entries = nil
				}
				for _, version := range page.versions {
					if !slices.ContainsFunc(history.entries, func(entry editorReleasedVersion) bool { return entry.tag == version.tag }) {
						history.entries = append(history.entries, version)
					}
				}
				if page.more {
					history.nextPage = history.loadingPage + 1
				} else {
					history.nextPage = -1
				}
			}
		}
	}
	state := m.editorUpdates()
	ui.Row(c.Part("history-header")).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, uiText("更新日志")).Bold().FontSize(16)
			ui.Text(c, uiText("当前版本")+" v"+version.Number).Muted().FontSize(12)
		})
		label, icon := uiText("检查更新"), checkUpdateIcon
		if state.staged != "" && state.download == nil {
			label, icon = uiText("查看已下载的更新"), updateAvailableIcon
		}
		if ui.Button(c.Part("check"), label).Icon(icon).Height(34).PadX(14).Radius(9).Shrink(0).
			Disabled(state.manual && state.check != nil).Clicked() {
			c.After(m.checkEditorUpdate)
		}
		if settingsExternalLink(c.Part("github"), "GitHub Releases").Clicked() {
			c.After(func() { m.openReleasePage(editorReleasesPage) })
		}
		label = uiText("刷新更新日志")
		if ui.Button(c.Part("refresh"), label).Icon(refreshIcon).IconOnly().Secondary().Size(34, 34).Radius(9).Shrink(0).
			Tooltip(label).Disabled(history.loading != nil).Clicked() {
			c.After(func() { history.load(1) })
		}
	})
	latest := ""
	for _, entry := range history.entries {
		if entry.prerelease {
			continue
		}
		if release, err := editorReleaseForTag(entry.tag, version.Number); err == nil {
			if release.comparison > 0 {
				latest = entry.tag
			}
			break
		}
	}
	for _, entry := range history.entries {
		m.releaseHistoryEntry(c.Part("release").Key(entry.tag), history, entry, entry.tag == latest)
	}
	switch {
	case history.loading != nil:
		ui.Text(c, uiText("正在读取版本历史…")).Muted().FontSize(12)
	case history.problem != "":
		ui.Wrap(c, history.problem).FillWidth().FontSize(12).Color(m.theme.Danger)
	case len(history.entries) == 0 && history.nextPage < 0:
		ui.Text(c, uiText("暂无公开版本。")).Muted().FontSize(12)
	}
	if history.loading == nil && (history.nextPage > 0 || history.problem != "") {
		label, next := "加载更多版本", history.nextPage
		if history.problem != "" {
			label, next = "重试", history.loadingPage
		}
		if ui.Button(c.Part("more"), uiText(label)).Outline().Height(32).Clicked() {
			c.After(func() { history.load(next) })
		}
	}
}

func (m *editorApp) releaseHistoryEntry(c ui.C, history *editorReleaseHistory, entry editorReleasedVersion, newVersion bool) {
	expanded := history.expanded == entry.tag
	ui.Column(c).FillWidth().Radius(10).Border(1, m.theme.Border).Background(m.theme.Background).Children(func() {
		header := ui.Row(c.Part("summary")).FillWidth().Height(42).PadX(12).Gap(10).Items(ui.Center).Radius(9)
		header.HitWith(ui.ID(c), entry.tag, gfx.HitOptions{Role: gfx.RoleButton, Selected: expanded}).
			Hover(ui.Look{}.Background(m.theme.Hover)).Focus(ui.Look{}.Background(m.theme.Hover))
		header.Children(func() {
			ui.Text(c, entry.tag).Bold().FontSize(14)
			if !entry.published.IsZero() {
				ui.Text(c, entry.published.Local().Format("2006-01-02")).FontSize(12).Muted()
			}
			ui.Slot(c).Grow(1)
			if strings.TrimPrefix(entry.tag, "v") == version.Number {
				ui.Text(c, uiText("当前版本")).FontSize(12).Color(m.theme.Success)
			} else if newVersion {
				ui.Text(c, uiText("有新版本")).FontSize(12).Color(m.theme.Success)
			} else if entry.prerelease {
				ui.Text(c, uiText("预发布")).FontSize(12).Muted()
			}
			indicator := chevronRightIcon
			if expanded {
				indicator = chevronDownIcon
			}
			ui.Icon(c, indicator).Size(16, 16).Color(m.theme.MutedForeground)
		})
		if header.Clicked() {
			c.After(func() {
				history.expanded = entry.tag
				if expanded {
					history.expanded = ""
				}
				history.notes = uimarkdown.State{}
			})
		}
		if !expanded {
			return
		}
		ui.Slot(c).FillWidth().Height(1).Background(m.theme.Border)
		ui.Column(c.Part("detail")).FillWidth().Pad(12).Gap(8).Children(func() {
			if entry.notes == "" {
				ui.Text(c, uiText("此版本没有更新说明。")).Muted().FontSize(12)
			} else {
				notes := &history.notes
				notes.Source = entry.notes
				notes.Style = uimarkdown.Style{Size: 13, LineHeight: 21}
				notes.Pointer = func(event gfx.PointerEvent) gfx.EventResult {
					index, link := notes.LinkIndex(event.Target)
					if !link || event.Kind != gfx.PointerUp || event.Button != gfx.PointerButtonPrimary {
						return gfx.EventResult{}
					}
					address, err := url.Parse(notes.Link(index))
					if err == nil && (address.Scheme == "https" || address.Scheme == "http") && address.Host != "" && address.User == nil {
						m.openReleasePage(address.String())
					}
					return gfx.EventResult{Handled: true}
				}
				height := max(24, notes.Height())
				uimarkdown.View(c.Part("notes"), uiText("更新内容"), notes).FillWidth().Height(height).DrawOver(func(d *ui.DrawContext) {
					if max(24, notes.Height()) != height {
						d.Canvas.RequestFrame()
					}
				})
			}
			if settingsExternalLink(c.Part("link"), "GitHub Release").Clicked() {
				c.After(func() { m.openReleasePage(entry.page) })
			}
		})
	})
}

func (m *editorApp) openReleasePage(page string) {
	if err := app.OpenURL(page); err != nil {
		m.appSettings.problem = uiText("无法打开链接：") + err.Error()
		return
	}
	m.appSettings.problem = ""
}
