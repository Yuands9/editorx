package editor

import (
	"fmt"
	"path/filepath"

	"github.com/vivaui/viva/ui"
)

func (m *editorApp) gitChangeList(c ui.C, staged, busy bool) {
	git := &m.tools.git
	var changes []gitChange
	for _, change := range git.snapshot.changes {
		if staged && change.staged() && !change.conflicted() || !staged && (change.unstaged() || change.conflicted()) {
			changes = append(changes, change)
		}
	}
	label, list, action := "工作区", &git.list, gitStage
	if staged {
		label, list, action = "已暂存", &git.indexList, gitUnstage
	}
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
		ui.Text(c, fmt.Sprintf("%s · %d", label, len(changes))).FontSize(12).Muted()
		ui.List(c.Part("files"), list, len(changes), func(row ui.ListRow) {
			change := changes[row.Index]
			row.Element.Height(29).Gap(3).Items(ui.Center).Children(func() {
				name := change.path
				if change.conflicted() {
					name = "冲突 · " + name
				}
				if ui.Button(row.C.Part("diff"), name).Icon(historyCompareIcon).Ghost().Height(27).Grow(1).MinWidth(0).Tooltip("查看差异 · " + change.path).Disabled(busy).Clicked() {
					c.After(func() {
						if change.conflicted() {
							m.openGitConflict(change)
						} else {
							m.openGitReview(change, staged)
						}
					})
				}
				if ui.Button(row.C.Part("open"), "打开文件").Icon(folderOpenIcon).IconOnly().Ghost().Size(27, 27).Tooltip("打开文件").Disabled(busy).Clicked() {
					c.After(func() { m.load(filepath.Join(git.snapshot.root, change.path)) })
				}
				icon := plusIcon
				if staged {
					icon = closeIcon
				}
				if ui.Button(row.C.Part("stage"), string(action)).Icon(icon).IconOnly().Ghost().Size(27, 27).Tooltip(string(action)).Disabled(busy || change.conflicted()).Clicked() {
					c.After(func() { m.startGit(action, "", change.paths()) })
				}
			})
		}).FillWidth().Grow(1).MinHeight(0).Label("Git " + label + "文件变更")
	})
}
