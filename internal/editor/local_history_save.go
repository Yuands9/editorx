package editor

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

func writeDocumentBytes(path string, data []byte, original os.FileInfo, history *localHistoryStore) ([sha256.Size]byte, error) {
	return writeDocumentAtomically(context.Background(), path, original, history, func(output io.Writer) error {
		_, err := output.Write(data)
		return err
	})
}

// The old contents are staged before replacement and become visible in history
// only after the document has been saved successfully.
func writeDocumentAtomically(ctx context.Context, path string, original os.FileInfo, history *localHistoryStore, write func(io.Writer) error) ([sha256.Size]byte, error) {
	var digest [sha256.Size]byte
	var draft *localHistoryDraft
	var historyErr error
	outputHash := sha256.New()

	err := writeFileAtomically(path, func(output io.Writer) error {
		if err := write(io.MultiWriter(output, outputHash)); err != nil {
			return err
		}
		if history != nil {
			var observed os.FileInfo
			draft, observed, historyErr = history.stage(ctx, path, time.Now())
			if errors.Is(historyErr, errDocumentChanged) {
				return historyErr
			}
			if original == nil {
				original = observed
			}
		}
		if original != nil {
			current, err := os.Stat(path)
			if err != nil || !sameTextFile(original, current) {
				return errDocumentChanged
			}
		}
		return ctx.Err()
	})
	if draft != nil {
		defer os.Remove(draft.temporary)
	}
	if err != nil {
		return digest, err
	}
	copy(digest[:], outputHash.Sum(nil))
	if draft != nil && draft.version.digest != digest {
		historyErr = history.commit(draft)
	}
	if history != nil && (draft != nil || historyErr != nil) {
		history.noteProblem(historyErr)
	}
	return digest, nil
}

func (m *editorApp) documentHistory() *localHistoryStore {
	if m.dataDirectory == "" {
		return nil
	}
	directory := filepath.Join(m.dataDirectory, localHistoryDirectory)
	if m.desktop != nil {
		if m.desktop.history == nil {
			m.desktop.history = &localHistoryStore{directory: directory, policy: m.desktop.preferences.LocalHistory}
		}
		return m.desktop.history
	}
	if m.localHistory == nil || m.localHistory.directory != directory {
		preferences, _ := m.applicationPreferences()
		m.localHistory = &localHistoryStore{directory: directory, policy: preferences.LocalHistory}
	}
	return m.localHistory
}

func (d *editorDesktop) maintainLocalHistory(now time.Time) {
	if d.history == nil || d.exiting || now.Before(d.nextHistoryCleanup) {
		return
	}
	d.nextHistoryCleanup = now.Add(time.Hour)
	d.historyWork.Go(func() {
		if _, err := d.history.clean(now, false); err != nil {
			d.history.noteProblem(err)
		}
	})
}
