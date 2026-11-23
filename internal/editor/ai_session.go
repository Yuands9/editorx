package editor

import (
	"crypto/sha256"

	"github.com/vivaui/viva/text"
)

type aiWindowSession struct {
	Open      bool
	ProfileID int    `json:",omitempty"`
	Model     string `json:",omitempty"`
	Current   aiConversationSession
	History   []aiConversationSession `json:",omitempty"`
}

type aiConversationSession struct {
	Name        string `json:",omitempty"`
	Mode        int
	Effort      aiReasoning           `json:",omitempty"`
	Draft       string                `json:",omitempty"`
	Turns       []aiTurnSession       `json:",omitempty"`
	Attachments []aiAttachmentSession `json:",omitempty"`
}

type aiAttachmentSession struct {
	Tab                  int    `json:",omitempty"`
	Path                 string `json:",omitempty"`
	Directory, Automatic bool
}

type aiTurnSession struct {
	Prompt, Provider, Model, Reply, Notice string
	Failed                                 bool              `json:",omitempty"`
	Changes                                []aiChangeSession `json:",omitempty"`
}

const (
	aiDiskSuggestion      = -1
	aiClosedTabSuggestion = -2
)

type aiChangeSession struct {
	// A positive file reference is a one-based position in the saved tab list. Zero is a new draft.
	FileRef                   int
	Path, Name, Format, Scope string
	Original, Value, Error    string
	Start, End                int
	SourceHash                [32]byte
}

func (m *editorApp) snapshotAI() *aiWindowSession {
	a := &m.ai
	current := a.currentConversation()
	if !a.conversationStarted && !a.open && !current.hasContent() && len(a.history) == 0 {
		return nil
	}
	tabPositions := make(map[int]int, len(m.tabs))
	for index, tab := range m.tabs {
		tabPositions[tab.id] = index + 1
	}
	profile := a.profiles.active(a.profileID)
	state := &aiWindowSession{Open: a.open, ProfileID: a.profileID, Current: current.snapshot(tabPositions)}
	if model := a.selectedModel(profile); model != profile.Model {
		state.Model = model
	}
	for _, record := range a.history {
		state.History = append(state.History, record.snapshot(tabPositions))
	}
	return state
}

func (h aiConversation) snapshot(tabs map[int]int) aiConversationSession {
	record := aiConversationSession{Name: h.name, Mode: h.mode, Effort: h.effort, Draft: h.draft}
	for _, item := range h.attachments {
		position := tabs[item.tabID]
		if item.tabID != 0 && position == 0 {
			continue
		}
		record.Attachments = append(record.Attachments, aiAttachmentSession{
			Tab: position, Path: item.path, Directory: item.directory, Automatic: item.automatic,
		})
	}
	for _, turn := range h.turns {
		saved := aiTurnSession{Prompt: turn.prompt, Provider: turn.provider, Model: turn.model, Reply: turn.reply,
			Notice: turn.notice, Failed: turn.failed}
		if turn.request != nil {
			saved.Notice, saved.Failed = "上次生成未完成，可重新发送", true
			if record.Draft == "" {
				record.Draft = turn.prompt
			}
		}
		for _, change := range turn.changes {
			target := change.target
			fileRef, hash := target.file.ID, target.baseHash
			switch {
			case fileRef > 0:
				fileRef = tabs[fileRef]
				if fileRef == 0 {
					fileRef = aiClosedTabSuggestion
				}
			case fileRef < 0:
				fileRef = aiDiskSuggestion
			}
			if fileRef != 0 && hash == ([32]byte{}) {
				hash = sha256.Sum256([]byte(target.base))
			}
			saved.Changes = append(saved.Changes, aiChangeSession{
				FileRef: fileRef, Path: target.path, Name: target.file.Name, Format: target.file.Format, Scope: target.file.Scope,
				Original: target.file.Text, Value: change.value, Error: change.error, Start: target.start, End: target.end, SourceHash: hash,
			})
		}
		record.Turns = append(record.Turns, saved)
	}
	return record
}

func (m *editorApp) restoreAI(state *aiWindowSession) {
	if state == nil {
		return
	}
	a := &m.ai
	a.open, a.conversationStarted, a.scrollEnd = state.Open, true, true
	a.profileID = state.ProfileID
	if err := m.reloadAIProfiles(); err != nil {
		a.message = err.Error()
	}
	profile := a.profiles.active(a.profileID)
	if profile.ID != 0 && profile.ID == state.ProfileID && validAIModelID(state.Model) {
		a.modelPicker = aiModelPickerState{profile: profile, model: state.Model}
	}
	a.aiConversation = state.Current.restore(m.tabs)
	a.prompt.Bind(text.NewEditor(a.draft))
	for _, record := range state.History {
		a.history = append(a.history, record.restore(m.tabs))
	}
}

func (saved aiConversationSession) restore(tabs []*editorTab) aiConversation {
	record := aiConversation{name: saved.Name, mode: saved.Mode, effort: saved.Effort, draft: saved.Draft}
	for _, item := range saved.Attachments {
		var tabID int
		if item.Tab != 0 {
			if item.Tab < 1 || item.Tab > len(tabs) || !aiTextTab(tabs[item.Tab-1]) {
				continue
			}
			tabID = tabs[item.Tab-1].id
		} else if item.Path == "" {
			continue
		}
		record.attachments = append(record.attachments, aiAttachment{
			tabID: tabID, path: item.Path, directory: item.Directory, automatic: item.Automatic,
		})
	}
	for _, item := range saved.Turns {
		turn := &aiTurn{prompt: item.Prompt, provider: item.Provider, model: item.Model, reply: item.Reply, notice: item.Notice, failed: item.Failed}
		for _, change := range item.Changes {
			fileID, problem := change.FileRef, change.Error
			if fileID > 0 {
				if fileID <= len(tabs) && aiTextTab(tabs[fileID-1]) {
					fileID = tabs[fileID-1].id
				} else {
					fileID = aiClosedTabSuggestion
				}
			}
			if fileID == aiClosedTabSuggestion {
				problem = "原标签已关闭，请重新生成修改建议"
			}
			turn.changes = append(turn.changes, aiChange{target: aiTarget{
				file: aiFileContext{ID: fileID, Name: change.Name, Format: change.Format, Scope: change.Scope, Text: change.Original},
				path: change.Path, start: change.Start, end: change.End, baseHash: change.SourceHash,
			}, value: change.Value, error: problem})
		}
		record.turns = append(record.turns, turn)
	}
	return record
}
