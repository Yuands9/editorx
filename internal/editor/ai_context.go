package editor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

type aiAttachment struct {
	tabID                int
	path                 string
	directory, automatic bool
}

type aiRequestContext struct {
	files   []aiTarget
	folders []string
	drafts  map[string]aiTarget
}

var errAINonTextFile = errors.New("AI 助手支持 UTF-8 文本文件")

func aiTextPath(path string) bool {
	extension := strings.ToLower(filepath.Ext(path))
	return !isImageFile(path) && officeKind(path) == "" && extension != ".doc" && extension != ".xls"
}

func aiPathName(path string) string {
	if filepath.Dir(path) == path {
		return path
	}
	return filepath.Base(path)
}

func aiRelativeTo(folder, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	relative, err := filepath.Rel(folder, path)
	return relative, err == nil && filepath.IsLocal(relative)
}

func (m *editorApp) aiAttachmentPath(item aiAttachment) string {
	if tab := m.aiTab(item.tabID); tab != nil {
		return tab.doc.path
	}
	return item.path
}

func (m *editorApp) aiAttachmentIndex(item aiAttachment) int {
	path := m.aiAttachmentPath(item)
	return slices.IndexFunc(m.ai.attachments, func(other aiAttachment) bool {
		if item.directory != other.directory {
			return false
		}
		if item.tabID != 0 && item.tabID == other.tabID {
			return true
		}
		return path != "" && strings.EqualFold(path, m.aiAttachmentPath(other))
	})
}

func (m *editorApp) addAIAttachment(item aiAttachment) {
	if index := m.aiAttachmentIndex(item); index >= 0 {
		if !item.automatic {
			m.ai.attachments[index] = item
		}
		return
	}
	m.ai.attachments = append(m.ai.attachments, item)
}

func (m *editorApp) removeAIAttachment(item aiAttachment) {
	if index := m.aiAttachmentIndex(item); index >= 0 {
		m.ai.attachments = slices.Delete(m.ai.attachments, index, index+1)
	}
}

func (m *editorApp) aiDefaultFolder(path string) string {
	if m.workspace == nil {
		return ""
	}
	var folder string
	for _, root := range m.workspace.roots {
		if _, inside := aiRelativeTo(root.path, path); inside && len(root.path) > len(folder) {
			folder = root.path
		}
	}
	if path == "" && m.active == nil && len(m.workspace.roots) == 1 {
		return m.workspace.roots[0].path
	}
	return folder
}

func (m *editorApp) setAIDefaultContext(tab *editorTab, path string, directory bool) {
	m.ai.attachments = slices.DeleteFunc(m.ai.attachments, func(item aiAttachment) bool { return item.automatic })
	if directory {
		m.addAIAttachment(aiAttachment{path: path, directory: true, automatic: true})
		return
	}
	if tab == nil && path != "" {
		tab = m.tabAtPath(path)
	}
	if tab != nil {
		path = tab.doc.path
		if aiTextTab(tab) {
			m.addAIAttachment(aiAttachment{tabID: tab.id, automatic: true})
		}
	} else if path != "" && aiTextPath(path) {
		m.addAIAttachment(aiAttachment{path: path, automatic: true})
	}
	if folder := m.aiDefaultFolder(path); folder != "" {
		m.addAIAttachment(aiAttachment{path: folder, directory: true, automatic: true})
	}
}

func aiTabTarget(tab *editorTab, useSelection bool) (aiTarget, error) {
	editor := tab.source.text
	if _, composing := editor.Composition(); composing {
		return aiTarget{}, fmt.Errorf("请先完成 %s 中的中文选词", tab.title())
	}
	base := editor.String()
	start, end, scope := 0, len(base), "整份文件"
	selected := editor.Selection()
	if useSelection && selected.Start != selected.End {
		if editor.SelectionCount() > 1 {
			return aiTarget{}, errors.New("请保留一个正文选区，或添加整份文件")
		}
		start = editor.ByteOffset(min(selected.Start, selected.End))
		end = editor.ByteOffset(max(selected.Start, selected.End))
		scope = "选区"
	}
	return aiTarget{
		file: aiFileContext{ID: tab.id, Name: tab.title(), Format: tab.format(), Scope: scope, Text: base[start:end]},
		path: tab.doc.path, base: base, start: start, end: end,
	}, nil
}

// Capture open editors on the UI thread. Folder traversal and disk reads run with the model request.
func (m *editorApp) captureAIContext() (aiRequestContext, error) {
	var snapshot aiRequestContext
	for _, item := range m.ai.attachments {
		if item.directory {
			snapshot.folders = append(snapshot.folders, item.path)
			continue
		}
		var tab *editorTab
		if item.tabID != 0 {
			tab = m.aiTab(item.tabID)
		} else {
			tab = m.tabAtPath(item.path)
		}
		if aiTextTab(tab) {
			target, err := aiTabTarget(tab, item.automatic)
			if err != nil {
				return aiRequestContext{}, err
			}
			snapshot.files = append(snapshot.files, target)
		} else if tab == nil && item.path != "" {
			snapshot.files = append(snapshot.files, aiTarget{path: item.path})
		}
	}
	if len(snapshot.folders) == 0 {
		return snapshot, nil
	}
	snapshot.drafts = make(map[string]aiTarget)
	for _, tab := range m.tabs {
		if !aiTextTab(tab) || tab.doc.path == "" {
			continue
		}
		for _, folder := range snapshot.folders {
			if _, inside := aiRelativeTo(folder, tab.doc.path); !inside {
				continue
			}
			target, err := aiTabTarget(tab, false)
			if err != nil {
				return aiRequestContext{}, err
			}
			snapshot.drafts[strings.ToLower(tab.doc.path)] = target
			break
		}
	}
	return snapshot, nil
}

func readAIFile(path string, id int) (aiTarget, error) {
	if !aiTextPath(path) {
		return aiTarget{}, errAINonTextFile
	}
	doc, err := readDocument(path)
	if err != nil {
		return aiTarget{}, err
	}
	return aiTarget{
		file: aiFileContext{ID: id, Name: filepath.Base(path), Format: documentFormat(path), Scope: "整份文件", Text: doc.saved},
		path: path, base: doc.saved, end: len(doc.saved),
	}, nil
}

func (snapshot aiRequestContext) targets(ctx context.Context) ([]aiTarget, error) {
	targets := make([]aiTarget, 0, len(snapshot.files))
	seenFiles, seenTabs := make(map[string]bool), make(map[int]bool)
	for _, target := range snapshot.files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := strings.ToLower(target.path)
		if key != "" && seenFiles[key] || target.file.ID != 0 && seenTabs[target.file.ID] {
			continue
		}
		if target.file.ID == 0 {
			path := target.path
			var err error
			target, err = readAIFile(path, -len(targets)-1)
			if err != nil {
				return nil, fmt.Errorf("无法读取附件 %s：%w", aiPathName(path), err)
			}
		}
		for _, folder := range snapshot.folders {
			if relative, inside := aiRelativeTo(folder, target.path); inside {
				target.file.Path = filepath.ToSlash(filepath.Join(aiPathName(folder), relative))
				break
			}
		}
		targets = append(targets, target)
		seenFiles[key], seenTabs[target.file.ID] = true, true
	}
	for _, folder := range snapshot.folders {
		err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path == folder && !entry.IsDir() {
				return errors.New("所选路径不是文件夹")
			}
			key := strings.ToLower(path)
			if !entry.Type().IsRegular() || seenFiles[key] {
				return nil
			}
			target, open := snapshot.drafts[key]
			if !open {
				target, err = readAIFile(path, -len(targets)-1)
				if errors.Is(err, errTextNotUTF8) || errors.Is(err, errTextHasNUL) || errors.Is(err, errAINonTextFile) {
					return nil
				}
				if err != nil {
					return err
				}
			}
			relative, _ := filepath.Rel(folder, path)
			target.file.Path = filepath.ToSlash(filepath.Join(aiPathName(folder), relative))
			targets, seenFiles[key] = append(targets, target), true
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("无法读取附件文件夹 %s：%w", aiPathName(folder), err)
		}
	}
	return targets, nil
}
