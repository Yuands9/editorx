package editor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type imageOCRAction int

const (
	imageOCRRecognize imageOCRAction = iota
	imageOCRDownload
	imageOCRRemove
)

type imageOCRDialog struct {
	open, installed, damaged, recognized bool
	directory, problem                   string
	output                               ui.Field
	action                               imageOCRAction
	task                                 *ui.EffectState[string, int]
	progress                             *dictionaryProgress
}

func (m *editorApp) showImageOCR() {
	directory := m.dataDirectory
	if directory == "" {
		var err error
		directory, err = editorDataDirectory()
		if err != nil {
			m.fail(err)
			return
		}
	}
	state := &imageOCRDialog{open: true, directory: directory}
	m.imageOCR = state
	state.refresh()
	if state.installed {
		m.startImageOCR(imageOCRRecognize)
	}
}

func (state *imageOCRDialog) refresh() {
	installation, err := readOfflineInstallation(state.directory, imageOCRComponent)
	if installation == nil && err == nil {
		_, err = os.Lstat(offlineComponentPath(state.directory, imageOCRComponent))
		if err == nil {
			err = errors.New("OCR 组件不完整，请移除后重新下载")
		} else if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	state.installed, state.damaged = installation != nil, err != nil
	if err != nil {
		state.problem = err.Error()
	}
}

func (m *editorApp) startImageOCR(action imageOCRAction) {
	state := m.imageOCR
	if state == nil || state.task != nil || m.active == nil || m.active.picture == nil {
		return
	}
	var progress *dictionaryProgress
	if action == imageOCRDownload {
		progress = &dictionaryProgress{invalidate: m.invalidator.Invalidate}
	}
	directory, bitmap := state.directory, m.active.picture.bitmap
	state.action, state.progress, state.problem = action, progress, ""
	state.task = ui.NewEffect[string, int](func(ctx context.Context) (string, error) {
		if !imageOCRComponentLock.TryLock() {
			return "", errors.New("另一个窗口正在使用 OCR，请稍后重试")
		}
		defer imageOCRComponentLock.Unlock()
		if action == imageOCRRemove {
			return "", os.RemoveAll(offlineComponentPath(directory, imageOCRComponent))
		}
		if action == imageOCRDownload {
			client := *editorUpdateHTTP
			client.Timeout = 15 * time.Minute
			client.CheckRedirect = func(request *http.Request, previous []*http.Request) error {
				if request.URL.Scheme != "https" || len(previous) >= 6 {
					return errors.New("OCR 组件下载跳转无效")
				}
				switch request.URL.Hostname() {
				case "www.python.org", "files.pythonhosted.org", "raw.githubusercontent.com":
					return nil
				default:
					return errors.New("OCR 组件下载跳转到了未知地址")
				}
			}
			if _, err := installOfflineComponent(ctx, &client, directory, imageOCRComponent, progress); err != nil {
				return "", err
			}
		}
		return recognizeImageText(ctx, directory, bitmap)
	})
}

func (m *editorApp) updateImageOCR(c ui.C) {
	state := m.imageOCR
	if state == nil || state.task == nil {
		return
	}
	ui.Effect(c, state.task, 0)
	result, err, done := state.task.Take()
	if !done {
		return
	}
	state.task, state.progress = nil, nil
	state.refresh()
	if err != nil {
		state.problem = err.Error()
		return
	}
	state.recognized = state.action != imageOCRRemove
	state.output.SetText(result)
}

func (m *editorApp) imageOCRView(c ui.C) {
	state := m.imageOCR
	if state == nil {
		return
	}
	busy := state.task != nil
	dismissed := ui.ModalWith(c, &state.open, "识别图片文字", ui.ModalSpec{Width: 620}, func() {
		if busy {
			label := "正在识别图片…"
			if state.action == imageOCRRemove {
				label = "正在移除 OCR 组件…"
			}
			if progress := state.progress; progress != nil && progress.received.Load() < imageOCRComponent.downloadBytes() {
				ui.Text(c, "正在下载 OCR 组件…").Muted()
				fraction := float32(progress.received.Load()) / float32(imageOCRComponent.downloadBytes())
				ui.Progress(c.Part("download"), fraction).FillWidth().Height(5)
			} else {
				ui.Text(c, label).Muted()
				ui.Progress(c.Part("busy"), 0).FillWidth().Height(5).Indeterminate()
			}
			return
		}
		if state.problem != "" {
			ui.Wrap(c, state.problem).FillWidth().Color(m.theme.Danger)
		}
		if state.recognized && state.output.Text() != "" {
			ui.TextArea(c.Part("text"), &state.output, "识别结果", "").FillWidth().Height(240)
		} else if state.recognized {
			ui.Text(c, "未识别到文字").Muted()
		} else if !state.installed && !state.damaged {
			ui.Text(c, "下载后可离线识别简体中文和英文。").Muted()
		}
	}, func() {
		if busy {
			if ui.Button(c, "取消").Ghost().Clicked() {
				m.imageOCR = nil
			}
			return
		}
		if state.installed || state.damaged {
			if ui.Button(c, "移除 OCR 组件").Ghost().Clicked() {
				c.After(func() { m.startImageOCR(imageOCRRemove) })
			}
			ui.Slot(c).Grow(1)
		}
		if state.installed {
			label := "重新识别"
			if !state.recognized {
				label = "识别"
			}
			if ui.Button(c, label).Outline().Clicked() {
				c.After(func() { m.startImageOCR(imageOCRRecognize) })
			}
		} else if !state.damaged {
			label := fmt.Sprintf("下载并识别 · %.1f MiB", float64(imageOCRComponent.downloadBytes())/(1<<20))
			if ui.Button(c, label).Clicked() {
				c.After(func() { m.startImageOCR(imageOCRDownload) })
			}
		}
		if state.recognized && strings.TrimSpace(state.output.Text()) != "" {
			if ui.Button(c, "复制全文").Icon(copyIcon).Clicked() {
				c.After(func() {
					if err := app.SetClipboardText(state.output.Text()); err != nil {
						state.problem = "复制失败：" + err.Error()
						return
					}
					state.problem = ""
				})
			}
		}
	})
	if dismissed {
		m.imageOCR = nil
	}
}
