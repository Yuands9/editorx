package editor

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
)

// The language applies to the entire EditorX process, including independent
// comparison windows and the status text produced by background jobs.
var englishInterface atomic.Bool

func setUILanguage(language menuLanguage) { englishInterface.Store(language == menuEnglish) }

func uiText(text string) string {
	if englishInterface.Load() {
		return menuEnglish.text(text)
	}
	return text
}

func currentUILanguage() menuLanguage {
	if englishInterface.Load() {
		return menuEnglish
	}
	return menuChinese
}

func (m *editorApp) applyLanguagePreference(language menuLanguage) {
	if m.menuLanguage == language {
		return
	}
	m.menuLanguage = language
	m.status = uiText("设置已保存")
	if state := m.historyCompare; state != nil {
		if state.window != nil {
			_ = state.window.SetTitle(state.windowTitle())
		}
		state.invalidator.Invalidate()
	}
	if state := m.gitLogWindow; state != nil {
		_ = state.window.SetTitle(state.logWindowTitle())
		state.invalidator.Invalidate()
	}
	script := previewLanguageJS(language)
	for _, tab := range m.tabs {
		if tab.office != nil && tab.office.view != nil {
			tab.office.view.Eval(script)
		}
		if tab.preview != nil && tab.preview.view != nil {
			tab.preview.view.Eval(script)
		}
	}
	for _, terminal := range m.tools.terminals {
		if terminal.view != nil {
			terminal.view.Eval(script)
		}
	}
}

// Marked controls belong to EditorX, so switching the interface language leaves
// document contents and the current preview position alone.
func previewLanguageJS(language menuLanguage) string {
	return `((language) => {
  if (window.editorxSetLanguage) { window.editorxSetLanguage(language); return; }
  const en = language === "en";
  document.documentElement.lang = en ? "en" : "zh-CN";
  for (const [target, attribute] of [["text", "textContent"], ["label", "aria-label"], ["title", "title"]]) {
    for (const node of document.querySelectorAll("[data-en-" + target + "]")) {
      const original = "data-zh-" + target;
      const isText = attribute === "textContent";
      if (!node.hasAttribute(original)) node.setAttribute(original, isText ? node.textContent : node.getAttribute(attribute));
      const value = node.getAttribute((en ? "data-en-" : "data-zh-") + target);
      if (isText) node.textContent = value;
      else node.setAttribute(attribute, value);
    }
  }
})(` + strconv.Quote(string(language)) + `)`
}

//go:embed assets/locales/en.json
var englishLabelsJSON []byte

var englishInterfaceLabels = sync.OnceValue(func() map[string]string {
	var labels map[string]string
	if err := json.Unmarshal(englishLabelsJSON, &labels); err != nil {
		panic(err)
	}
	return labels
})
