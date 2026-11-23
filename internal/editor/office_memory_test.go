package editor

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func editedDocx(t *testing.T, original []byte, text string) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(original), int64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, part := range reader.File {
		stream, err := part.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(stream)
		stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		if part.Name == "word/document.xml" {
			body = bytes.ReplaceAll(body, []byte("中文文档测试"), []byte(text))
		}
		entry, err := writer.Create(part.Name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestOfficeCompactDraftCompatibility(t *testing.T) {
	original := sampleDocx(t)
	draft := editedDocx(t, original, "旧版本保存的中文草稿")
	office, err := restoreOffice(officeSession{Kind: "docx", Original: original, Snapshot: draft, Dirty: true}, "草稿.docx")
	if err != nil {
		t.Fatal(err)
	}
	saved := office.snapshotSession()
	if len(saved.Snapshot) != 0 || !bytes.Equal(saved.Original, draft) || !saved.Dirty {
		t.Fatal("legacy draft was not compacted")
	}
	// A background save must keep its captured bytes while a newer export replaces the live draft.
	next := editedDocx(t, original, "下一次编辑")
	office.received = next
	office.acceptSnapshot()
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var restoredState officeSession
	if err = json.Unmarshal(encoded, &restoredState); err != nil {
		t.Fatal(err)
	}
	restored, err := restoreOffice(restoredState, "草稿.docx")
	if err != nil {
		t.Fatal(err)
	}
	data, err := restored.output()
	if err != nil || !bytes.Equal(data, draft) {
		t.Fatalf("in-flight session changed: %v", err)
	}
	data, err = office.output()
	if err != nil || !bytes.Equal(data, next) {
		t.Fatalf("latest export lost: %v", err)
	}
}

func TestOfficeSnapshotServerReplacesOldContent(t *testing.T) {
	original := sampleDocx(t)
	office, err := restoreOffice(officeSession{Kind: "docx", Original: original}, "草稿.docx")
	if err != nil {
		t.Fatal(err)
	}
	if err = office.serve("草稿.docx"); err != nil {
		t.Fatal(err)
	}
	defer office.close()
	latest := editedDocx(t, original, "服务器最新版本")
	post := func(revision string, body []byte) {
		t.Helper()
		request, _ := http.NewRequest("POST", office.url+"snapshot?revision="+revision, bytes.NewReader(body))
		request.Header.Set("Origin", strings.TrimSuffix(strings.Split(office.url, "/")[0]+"//"+strings.Split(office.url, "/")[2], "/"))
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent {
			t.Fatal(response.Status)
		}
	}
	post("2", latest)
	office.acceptSnapshot()
	post("1", original)
	response, err := http.Get(office.url + "document")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || !bytes.Equal(body, latest) {
		t.Fatalf("reload did not receive latest snapshot: %v", err)
	}
	response, err = http.Get(office.url + "info")
	if err != nil {
		t.Fatal(err)
	}
	var info struct{ Revision int }
	err = json.NewDecoder(response.Body).Decode(&info)
	response.Body.Close()
	if err != nil || info.Revision != 2 {
		t.Fatalf("reload revision=%d: %v", info.Revision, err)
	}
	office.close()
	if office.serverContent != nil || office.received != nil {
		t.Fatal("closed server retained document buffers")
	}
	data, err := office.output()
	if err != nil || !bytes.Equal(data, latest) {
		t.Fatalf("close discarded session draft: %v", err)
	}
	if response, err = http.Get(office.url + "info"); err == nil {
		response.Body.Close()
		t.Fatal("closed document still serves HTTP")
	}
}

func TestOfficeFlushTimerStopsAtCompletion(t *testing.T) {
	calls := 0
	office := &officeDocument{request: 3, pending: func(err error) {
		calls++
		if err != nil {
			t.Error(err)
		}
	}}
	timer := time.AfterFunc(time.Hour, func() {})
	office.flushTimer = timer
	office.finishFlush(2, nil)
	if calls != 0 || office.pending == nil {
		t.Fatal("stale completion consumed current request")
	}
	office.finishFlush(3, nil)
	if calls != 1 || office.pending != nil || office.flushTimer != nil || timer.Stop() {
		t.Fatal("completed request retained timer/callback")
	}
	office.finishFlush(3, nil)
	if calls != 1 {
		t.Fatal("completion called twice")
	}
	timer = time.AfterFunc(time.Hour, func() {})
	office.flushTimer, office.pending = timer, func(error) { calls++ }
	office.close()
	if office.pending != nil || office.flushTimer != nil || timer.Stop() || calls != 1 {
		t.Fatal("close retained pending request")
	}
}
