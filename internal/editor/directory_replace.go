package editor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/vivaui/viva/text/find"
	"github.com/vivaui/viva/ui"
)

type directoryReplacementFile struct {
	path             string
	digest, diskHash [32]byte
	count            int
	preview          string
	selected         bool
}
type directoryReplacementPlan struct {
	query    *find.Query
	template string
	files    []directoryReplacementFile
	list     ui.ListState
}
type directoryReplacementJob struct {
	index   int
	file    directoryReplacementFile
	tab     *editorTab
	before  string
	request *ui.EffectState[string, int]
}

func readDirectoryDocument(ctx context.Context, path string) (document, error) {
	file, err := os.Open(path)
	if err != nil {
		return document{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(largeEditContextReader{ctx: ctx, Reader: file}, directorySearchFileLimit+1))
	if err != nil {
		return document{}, err
	}
	if len(data) > directorySearchFileLimit {
		return document{}, errors.New(uiText("文件超过 64 MiB"))
	}
	return decodeDocument(path, data)
}

func planDirectoryReplacement(ctx context.Context, paths []string, query *find.Query, template string, buffers map[string]string) (*directoryReplacementPlan, error) {
	plan := &directoryReplacementPlan{query: query, template: template}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, err := readDirectoryDocument(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("%s：%w", path, err)
		}
		value := doc.saved
		if buffer, ok := buffers[strings.ToLower(path)]; ok {
			value = buffer
		}
		edit, count := query.ReplaceAllEdit(value, template)
		if count == 0 {
			continue
		}
		before := strings.ToValidUTF8(value[max(0, edit.Start-40):min(len(value), edit.Start+120)], "")
		after := strings.ToValidUTF8(edit.Text[:min(len(edit.Text), 120)], "")
		plan.files = append(plan.files, directoryReplacementFile{path: path, digest: sha256.Sum256([]byte(value)), diskHash: doc.diskHash, count: count, preview: strings.ReplaceAll(before+" → "+after, "\n", " ↵ "), selected: true})
	}
	return plan, nil
}

func (m *editorApp) previewDirectoryReplacement() {
	s := m.directorySearch
	if s == nil || s.result.query == nil || s.request != nil || s.planRequest != nil || s.replacing || s.replaceJob != nil {
		return
	}
	if s.result.partial {
		s.problem = uiText("搜索结果不完整，请先缩小范围再预览替换")
		return
	}
	var paths []string
	seen := map[string]bool{}
	for _, hit := range s.result.hits {
		key := strings.ToLower(hit.path)
		if !seen[key] {
			seen[key] = true
			paths = append(paths, hit.path)
		}
	}
	buffers := map[string]string{}
	for _, tab := range m.tabs {
		if historyEditableTab(tab) && seen[strings.ToLower(tab.doc.path)] {
			if tab.format() == "JSON" {
				tab.updateResult()
			}
			buffers[strings.ToLower(tab.doc.path)] = tab.source.text.CommittedString()
		}
	}
	query, template := s.result.query, s.replacement.Text()
	s.problem, s.plan, s.replaceProblems = "", nil, nil
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.planRequest = ui.NewEffect[*directoryReplacementPlan, int](func(effectContext context.Context) (*directoryReplacementPlan, error) {
		stop := context.AfterFunc(effectContext, cancel)
		defer stop()
		defer cancel()
		return planDirectoryReplacement(ctx, paths, query, template, buffers)
	})
	m.invalidator.Invalidate()
}

func (m *editorApp) startDirectoryReplacement() {
	s := m.directorySearch
	if s == nil || s.plan == nil || s.replacing || s.replaceJob != nil || s.plan.template != s.replacement.Text() {
		return
	}
	s.replacing, s.replaceIndex, s.replacedFiles, s.replaceProblems, s.problem = true, 0, 0, nil, ""
	m.invalidator.Invalidate()
}

func (m *editorApp) updateDirectoryReplacement(c ui.C) {
	s := m.directorySearch
	if s.planRequest != nil {
		ui.Effect(c.Part("preview"), s.planRequest, 0)
		if plan, err, done := s.planRequest.Take(); done {
			s.planRequest, s.cancel = nil, nil
			if err != nil {
				s.problem = err.Error()
			} else {
				s.plan = plan
			}
		}
	}
	report := func(path string, err error) {
		if len(s.replaceProblems) < 8 {
			s.replaceProblems = append(s.replaceProblems, filepath.Base(path)+"："+err.Error())
		}
	}
	if job := s.replaceJob; job != nil {
		ui.Effect(c.Part("apply").KeyInt(job.index), job.request, 0)
		value, err, done := job.request.Take()
		if !done {
			return
		}
		s.replaceJob = nil
		if err == nil && job.tab != nil {
			if !m.hasTab(job.tab) || job.tab.source.text.CommittedString() != job.before || job.tab.source.text.CompositionActive() {
				err = errors.New(uiText("编辑内容已变化，已跳过"))
			} else {
				job.tab.source.syncJSONText(value, false)
				job.tab.projectPreview = false
			}
		}
		if err != nil {
			report(job.file.path, err)
		} else {
			s.replacedFiles++
		}
		m.checkFiles = true
		m.invalidator.Invalidate()
	}
	if !s.replacing || s.plan == nil {
		return
	}
	for s.replaceIndex < len(s.plan.files) {
		file := s.plan.files[s.replaceIndex]
		s.replaceIndex++
		if !file.selected {
			continue
		}
		job := &directoryReplacementJob{file: file, tab: m.tabAtPath(file.path), index: s.replaceIndex}
		if job.tab != nil {
			if !historyEditableTab(job.tab) || job.tab.savingText != nil || job.tab.jsonComposing() {
				report(file.path, errors.New(uiText("当前文件不可替换，已跳过")))
				continue
			}
			if job.tab.format() == "JSON" {
				job.tab.updateResult()
			}
			job.before = job.tab.source.text.CommittedString()
			if sha256.Sum256([]byte(job.before)) != file.digest {
				report(file.path, errors.New(uiText("预览后内容已变化，已跳过")))
				continue
			}
		}
		query, template, history := s.plan.query, s.plan.template, m.documentHistory()
		job.request = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
			if job.tab != nil {
				value, _ := query.ReplaceAll(job.before, template)
				start, end, replacement := trimHistoryReplacement(job.before, 0, len(job.before), value)
				if utf8.RuneCountInString(job.before[start:end])+utf8.RuneCountInString(replacement) > historyMergeUndoRunes {
					return "", errors.New(uiText("替换超过可撤销上限，请缩小替换范围"))
				}
				return value, ctx.Err()
			}
			doc, err := readDirectoryDocument(ctx, file.path)
			if err != nil {
				return "", err
			}
			if doc.diskHash != file.diskHash || sha256.Sum256([]byte(doc.saved)) != file.digest {
				return "", errors.New(uiText("预览后磁盘文件已变化，已跳过"))
			}
			value, _ := query.ReplaceAll(doc.saved, template)
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "", doc.save(file.path, value, history)
		})
		s.replaceJob = job
		ui.Effect(c.Part("apply").KeyInt(job.index), job.request, 0)
		return
	}
	s.replacing = false
	s.problem = fmt.Sprintf(uiText("替换完成 · %d 个文件；已打开文件的修改尚未保存。"), s.replacedFiles)
	s.plan = nil
}
