package editor

import (
	"fmt"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) updateSettings(c ui.C) {
	m.updateMirrorSettings(c.Part("download-source"))

	enabled := !m.appSettings.draft.DisableAutoUpdate
	schedule := fmt.Sprintf(uiText("每次启动约 %g 秒后检查，运行期间每 %g 小时检查一次。"),
		editorUpdateStartupDelay.Seconds(), editorUpdateInterval.Hours())
	if m.applicationSettingSwitch(c.Part("auto-update"), uiText("自动检查更新"), schedule, &enabled) {
		m.appSettings.draft.DisableAutoUpdate = !enabled
		c.After(m.saveApplicationSettings)
	}

	m.releaseHistorySettings(c.Part("release-history"))
}
