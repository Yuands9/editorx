package editor

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
)

var workflowActions = []struct{ id, label string }{
	{"compare", "通用分屏对比…"}, {"follow", "跟随 / 暂停日志"}, {"check-disk", "检查外部变更"},
	{"record", "开始 / 停止录制宏"}, {"macros", "宏管理与重放…"}, {"tools", "自定义工具…"},
}

func (m *editorApp) workflowEnabled(id string) bool {
	switch id {
	case "follow":
		return m.active != nil && watchedTextTab(m.active) && !m.active.modified() && !m.active.hasResultDraft()
	case "record":
		return m.macro != nil || historyEditableTab(m.active)
	}
	return true
}
func (m *editorApp) runWorkflow(id string) {
	if m.macro != nil && id != "record" {
		if m.macro.editor.CompositionActive() {
			return
		}
		m.stopMacroRecording()
	}
	switch id {
	case "compare":
		m.openTextComparePicker()
	case "follow":
		m.toggleLogFollow(m.active)
	case "check-disk":
		m.checkFiles = true
		m.status = uiText("正在检查磁盘文件")
		m.invalidator.Invalidate()
	case "record":
		if m.macro == nil {
			m.startMacroRecording()
		} else if !m.macro.editor.CompositionActive() {
			if m.stopMacroRecording() {
				m.openWorkflowDialog("宏")
			}
		}
	case "macros":
		m.openWorkflowDialog("宏")
	case "tools":
		m.openWorkflowDialog("自定义工具")
	}
}

type workflowDialog struct {
	toolOutput                                             string
	replay                                                 *macroReplayJob
	open                                                   bool
	section                                                string
	preferences                                            workflowPreferences
	selected                                               int
	name, executable, arguments, directory, repeat, output ui.Field
	selectionInput                                         bool
	macro                                                  editorMacro
	problem, notice                                        string
	run                                                    *ui.EffectState[toolRunResult, int]
	cancel                                                 context.CancelFunc
}

func (m *editorApp) openWorkflowDialog(section string) {
	if m.dataDirectory == "" {
		directory, err := editorDataDirectory()
		if err != nil {
			m.fail(err)
			return
		}
		m.dataDirectory = directory
	}
	prefs, err := readWorkflowPreferences(m.dataDirectory)
	if err != nil {
		m.fail(err)
		return
	}
	s := &workflowDialog{open: true, section: section, preferences: prefs, selected: -1}
	s.output.ReadOnly = true
	s.repeat.SetText("1")
	if section == "宏" && m.lastMacro != nil {
		s.macro = *m.lastMacro
		s.name.SetText(m.lastMacro.Name)
	}
	m.workflow = s
}

func (s *workflowDialog) selectItem() {
	s.problem, s.notice = "", ""
	if s.section == "宏" {
		if s.selected < 0 {
			s.macro = editorMacro{}
		} else {
			s.macro = s.preferences.Macros[s.selected]
		}
		s.name.SetText(s.macro.Name)
		return
	}
	tool := customEditorTool{}
	if s.selected >= 0 {
		tool = s.preferences.Tools[s.selected]
	}
	s.name.SetText(tool.Name)
	s.executable.SetText(tool.Executable)
	s.arguments.SetText(strings.Join(tool.Arguments, "\n"))
	s.arguments.GetEditor().MoveTo(0, false)
	s.directory.SetText(tool.Directory)
	s.selectionInput = tool.SelectionInput
}

func (s *workflowDialog) tool() customEditorTool {
	var args []string
	if value := strings.ReplaceAll(s.arguments.Text(), "\r\n", "\n"); value != "" {
		args = strings.Split(value, "\n")
	}
	return customEditorTool{Name: strings.TrimSpace(s.name.Text()), Executable: strings.TrimSpace(s.executable.Text()), Arguments: args, Directory: s.directory.Text(), SelectionInput: s.selectionInput}
}

func (m *editorApp) saveWorkflowItem(remove bool) {
	s := m.workflow
	if s == nil {
		return
	}
	latest, err := readWorkflowPreferences(m.dataDirectory)
	if err != nil {
		s.problem = err.Error()
		return
	}
	name := strings.TrimSpace(s.name.Text())
	if s.section == "宏" {
		oldName := ""
		if s.selected >= 0 {
			oldName = s.preferences.Macros[s.selected].Name
		}
		index := slices.IndexFunc(latest.Macros, func(item editorMacro) bool { return item.Name == oldName })
		duplicate := slices.IndexFunc(latest.Macros, func(item editorMacro) bool { return item.Name == name })
		if !remove && duplicate >= 0 && duplicate != index {
			s.problem = uiText("已有同名宏，请更换名称")
			return
		}
		if remove {
			if index >= 0 {
				latest.Macros = slices.Delete(latest.Macros, index, index+1)
			}
			s.selected = -1
		} else {
			macro := s.macro
			macro.Name = name
			if index < 0 {
				latest.Macros = append(latest.Macros, macro)
				index = len(latest.Macros) - 1
			} else {
				latest.Macros[index] = macro
			}
			s.selected = index
		}
	} else {
		oldName := ""
		if s.selected >= 0 {
			oldName = s.preferences.Tools[s.selected].Name
		}
		index := slices.IndexFunc(latest.Tools, func(item customEditorTool) bool { return item.Name == oldName })
		duplicate := slices.IndexFunc(latest.Tools, func(item customEditorTool) bool { return item.Name == name })
		if !remove && duplicate >= 0 && duplicate != index {
			s.problem = uiText("已有同名工具，请更换名称")
			return
		}
		if remove {
			if index >= 0 {
				latest.Tools = slices.Delete(latest.Tools, index, index+1)
			}
			s.selected = -1
		} else {
			if index < 0 {
				latest.Tools = append(latest.Tools, s.tool())
				index = len(latest.Tools) - 1
			} else {
				latest.Tools[index] = s.tool()
			}
			s.selected = index
		}
	}
	if err := latest.save(m.dataDirectory); err != nil {
		s.problem = err.Error()
		s.selected = -1
		return
	}
	s.preferences = latest
	s.selectItem()
	s.notice = uiText("已保存")
}

func (m *editorApp) runWorkflowTool() {
	s := m.workflow
	if s == nil || s.run != nil {
		return
	}
	tool := s.tool()
	if err := (workflowPreferences{Tools: []customEditorTool{tool}}).validate(); err != nil {
		s.problem = err.Error()
		return
	}
	values := m.currentToolContext()
	if len(values.selection) > toolOutputLimit {
		s.problem = uiText("选区超过 1 MiB，请缩小工具输入")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.problem, s.notice = "", uiText("正在运行…")
	s.output.SetText("")
	s.toolOutput = ""
	s.run = ui.NewEffect[toolRunResult, int](func(effectCtx context.Context) (toolRunResult, error) {
		stop := context.AfterFunc(effectCtx, cancel)
		defer stop()
		defer cancel()
		return runCustomTool(ctx, tool, values)
	})
}

func (m *editorApp) workflowDialogView(c ui.C) {
	s := m.workflow
	if s == nil {
		return
	}
	if s.run != nil {
		ui.Effect(c.Part("run"), s.run, 0)
		if result, err, done := s.run.Take(); done {
			s.run, s.cancel = nil, nil
			s.toolOutput = result.output
			preview := result.output
			if len(preview) > 16<<10 {
				end := 16 << 10
				for end > 0 && !utf8.RuneStart(preview[end]) {
					end--
				}
				preview = preview[:end] + "\n…（打开新标签查看完整输出）"
			}
			s.output.SetText(preview)
			s.output.GetEditor().MoveTo(0, false)
			s.notice = uiText("运行完成")
			if result.truncated {
				s.notice += uiText(" · 仅保留前 1 MiB 输出")
			}
			if err != nil {
				s.problem = uiText("运行结束：") + err.Error()
			}
		}
	}
	m.updateMacroReplay(c.Part("macro-replay"), s)
	if m.workflow == nil {
		return
	}
	busy := s.run != nil || s.replay != nil
	choices := []int{-1}
	count := len(s.preferences.Tools)
	if s.section == "宏" {
		count = len(s.preferences.Macros)
	}
	for i := 0; i < count; i++ {
		choices = append(choices, i)
	}
	dismissed := ui.ModalWith(c, &s.open, s.section, ui.ModalSpec{Width: min(700, c.Bounds().Width-32)}, func() {
		height := float32(210)
		if s.section != "宏" {
			height = 365
			if s.output.Text() != "" {
				height += 170
			}
		}
		ui.Scroll(c.Part("content")).FillWidth().Height(min(height, max(180, c.Bounds().Height-180))).Scrollbars().Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				if ui.Select(c.Part("saved"), &s.selected, choices, func(index int) string {
					if index < 0 {
						return "新建 / 当前录制"
					}
					if s.section == "宏" {
						return s.preferences.Macros[index].Name
					}
					return s.preferences.Tools[index].Name
				}).Label(uiText("已保存的") + s.section).FillWidth().Height(32).Disabled(busy).Changed() {
					s.selectItem()
				}
				ui.Row(c.Part("name-row")).FillWidth().Items(ui.Center).Gap(8).Children(func() {
					ui.Text(c, uiText("名称")).FontSize(12).Width(64).Shrink(0)
					ui.TextField(c.Part("name"), &s.name, uiText("名称"), uiText("名称")).Grow(1).MinWidth(0).Height(32).Disabled(busy)
				})
				if s.section == "宏" {
					ui.Wrap(c, fmt.Sprintf(uiText("%d 步 · 录制键盘输入、粘贴、删除、缩进与光标移动。使用单光标；上下移动按逻辑行执行，鼠标选区按相对位置重放。"), len(s.macro.Steps))).FontSize(12).Muted()
					ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
						ui.Text(c, uiText("重复次数")).FontSize(12)
						ui.TextField(c.Part("repeat"), &s.repeat, uiText("重复次数"), "1–100").Width(90).Height(32)
						ui.Slot(c).Grow(1)
						if ui.Button(c, uiText("开始录制")).Ghost().Disabled(busy || !historyEditableTab(m.active) || m.macro != nil).Clicked() {
							c.After(func() { m.workflow = nil; m.startMacroRecording() })
						}
						if ui.Button(c, uiText("重放宏")).Disabled(busy || len(s.macro.Steps) == 0 || !historyEditableTab(m.active) || m.macro != nil).Clicked() {
							c.After(m.startMacroReplay)
						}
					})
				} else {
					ui.Row(c.Part("executable-row")).FillWidth().Items(ui.Center).Gap(8).Children(func() {
						ui.Text(c, uiText("程序")).FontSize(12).Width(64).Shrink(0)
						ui.TextField(c.Part("executable"), &s.executable, uiText("程序"), uiText("程序名或完整可执行文件路径")).Grow(1).MinWidth(0).Height(32).Disabled(busy)
					})
					ui.Text(c, uiText("参数 · 每行一个，无需给路径加引号")).FontSize(12).Muted()
					ui.TextArea(c.Part("arguments"), &s.arguments, uiText("参数（每行一个）"), uiText("参数，每行一个")).FillWidth().Height(80).Disabled(busy)
					ui.Row(c.Part("directory-row")).FillWidth().Items(ui.Center).Gap(8).Children(func() {
						ui.Text(c, uiText("工作目录")).FontSize(12).Width(64).Shrink(0)
						ui.TextField(c.Part("directory"), &s.directory, uiText("工作目录"), uiText("留空使用当前文件或项目目录")).Grow(1).MinWidth(0).Height(32).Disabled(busy)
					})
					ui.Wrap(c, uiText("变量：{file} 文件路径、{dir} 所在目录、{project} 首个项目、{selection} 选区。仅点击运行时执行；管道或脚本请显式指定解释器。")).FontSize(11).Muted()
					ui.Row(c).FillWidth().Items(ui.Center).Gap(8).Children(func() {
						ui.Checkbox(c, &s.selectionInput, uiText("将选区作为标准输入")).Disabled(busy)
						ui.Slot(c).Grow(1)
						if busy {
							if ui.Button(c, uiText("停止工具")).Clicked() && s.cancel != nil {
								s.cancel()
							}
						} else if ui.Button(c, uiText("运行工具")).Clicked() {
							c.After(m.runWorkflowTool)
						}
					})
					if s.output.Text() != "" {
						ui.TextArea(c.Part("output"), &s.output, uiText("工具输出"), "").FillWidth().Height(min(130, max(50, c.Bounds().Height-555)))
						if ui.Button(c, uiText("将输出打开为新标签")).Ghost().Clicked() {
							c.After(func() {
								tab := m.addTab(document{}, "工具输出.txt")
								tab.source.replace(s.toolOutput)
								m.workflow = nil
							})
						}
					}
				}
				if s.problem != "" {
					ui.Wrap(c, s.problem).FontSize(12).Color(m.theme.Danger)
				} else if s.notice != "" {
					ui.Wrap(c, s.notice).FontSize(12).Muted()
				}
			})
		})
	}, func() {
		if ui.Button(c, uiText("删除此")+s.section).Ghost().Disabled(busy || s.selected < 0).Clicked() {
			c.After(func() { m.saveWorkflowItem(true) })
		}
		if ui.Button(c, uiText("保存")+s.section).Disabled(busy || s.section == "宏" && len(s.macro.Steps) == 0).Clicked() {
			c.After(func() { m.saveWorkflowItem(false) })
		}
	})
	if dismissed || !s.open {
		if s.cancel != nil {
			s.cancel()
		}
		m.workflow = nil
		m.focusActiveSource()
	}
}
