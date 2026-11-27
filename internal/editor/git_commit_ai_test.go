package editor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
)

func gitCommitAIUIFixture(t *testing.T) (string, *editorApp, *headless.Session, func(...string) string) {
	t.Helper()
	root, git := gitReviewFixture(t)
	for name, value := range map[string]string{"selected.txt": "before\n", "excluded.txt": "private base\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", ".")
	git("commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(root, "selected.txt"), []byte("selected staged\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "selected.txt")
	for name, value := range map[string]string{"selected.txt": "selected staged\nselected disk\n", "excluded.txt": "PRIVATE_UNSELECTED_SENTINEL\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	git("add", "excluded.txt")
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.addTab(document{saved: "UNRELATED_OPEN_DOCUMENT_SENTINEL"}, "unrelated.txt")
	m.openProjectGit(root)
	s := openTestSession(t, m)
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("checkbox:" + uiText("选择提交 · ") + "selected.txt"); err != nil {
		t.Fatal(err)
	}
	return root, m, s, git
}

func TestGitCommitAIGeneratesOnlySelectedChanges(t *testing.T) {
	for _, language := range []menuLanguage{menuChinese, menuEnglish} {
		t.Run(string(language), func(t *testing.T) {
			setUILanguage(language)
			t.Cleanup(func() { setUILanguage(menuChinese) })
			root, m, s, git := gitCommitAIUIFixture(t)
			m.menuLanguage = language
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			beforeHead := git("rev-parse", "HEAD")
			beforeIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
			check(err)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer commit-test-token" || r.Header.Get("X-Commit-Test") != "configured-header" {
					t.Error("generation did not use the selected profile endpoint, key and headers")
				}
				var request struct {
					Model    string
					Messages []aiMessage
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "commit-test-model" || len(request.Messages) != 2 {
					t.Error("unexpected generation request", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				system, input := request.Messages[0].Content, request.Messages[1].Content
				if request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
					t.Error("request must separate instructions from repository content")
				}
				for _, value := range []string{"selected.txt", "before", "selected staged", "selected disk"} {
					if !strings.Contains(input, value) {
						t.Error("request missed a selected HEAD-to-disk change:", value)
					}
				}
				for _, value := range []string{"PRIVATE_UNSELECTED_SENTINEL", "UNRELATED_OPEN_DOCUMENT_SENTINEL", "excluded.txt", root} {
					if strings.Contains(input, value) {
						t.Error("request included unrelated content or an absolute workspace path:", value)
					}
				}
				wantedLanguage := "Chinese"
				if language == menuEnglish {
					wantedLanguage = "English"
				}
				if !strings.Contains(system, wantedLanguage) {
					t.Error("generation instructions did not follow the interface language")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"更新所选文本\n\n保留已暂存和工作区的改动"},"finish_reason":"stop"}]}`)
			}))
			defer server.Close()
			profile := aiSettings{ID: 1, Name: "提交服务", Provider: "custom", BaseURL: server.URL, Model: "commit-test-model", Protocol: aiChat}
			profile.Key, err = windowsAIKey([]byte("commit-test-token"), true)
			check(err)
			profile.Headers, err = windowsAIKey([]byte(`{"X-Commit-Test":"configured-header"}`), true)
			check(err)
			check((aiProfiles{DefaultID: 1, Profiles: []aiSettings{profile}}).save(m.dataDirectory))
			m.tools.git.commit.SetText("my original draft")
			check(s.Frame())
			if calls.Load() != 0 {
				t.Fatal("opening Git or selecting a file sent an AI request")
			}
			check(s.ClickLabel("button:" + uiText("AI 生成提交说明")))
			check(s.WaitFor(func(app.Snapshot) bool { return m.tools.git.commitAI.request == nil }, 10*time.Second))
			if calls.Load() != 1 || m.tools.git.commitAI.problem != "" || m.tools.git.commit.Text() != "更新所选文本\n\n保留已暂存和工作区的改动" {
				t.Fatalf("generation failed: calls=%d issue=%q text=%q", calls.Load(), m.tools.git.commitAI.problem, m.tools.git.commit.Text())
			}
			if s.HasText(uiText("勾选文件以加入提交")) || s.HasText(uiText("提交所选文件的全部已保存改动")) {
				t.Fatal("removed explanatory lines are still visible")
			}
			check(s.ClickLabel(uiText("提交说明")))
			_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'z')
			check(err)
			if m.tools.git.commit.Text() != "my original draft" {
				t.Fatal("undo could not restore the handwritten commit message")
			}
			_, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'y')
			check(err)
			if !strings.HasPrefix(m.tools.git.commit.Text(), "更新所选文本") {
				t.Fatal("redo could not restore the generated message")
			}
			check(s.Type(" edited"))
			if !strings.Contains(m.tools.git.commit.Text(), " edited") {
				t.Fatal("generated message is not editable")
			}
			afterIndex, err := os.ReadFile(filepath.Join(root, ".git", "index"))
			check(err)
			if beforeHead != git("rev-parse", "HEAD") || !bytes.Equal(beforeIndex, afterIndex) {
				t.Fatal("generating a message changed HEAD or the Git index")
			}
			if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
				check(s.Resize(900, 560))
				for range 6 {
					check(s.Frame())
				}
				check(s.Screenshot(filepath.Join(output, "git-ai-"+string(language)+".png")))
			}
		})
	}
}

func TestGitCommitAIFailurePreservesDraft(t *testing.T) {
	_, m, s, _ := gitCommitAIUIFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	profile := aiSettings{ID: 1, Name: "失败服务", Provider: "custom", BaseURL: server.URL, Model: "test", Protocol: aiChat, Auth: aiAuthNone}
	if err := (aiProfiles{Profiles: []aiSettings{profile}}).save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}
	m.tools.git.commit.SetText("keep my draft")
	if err := s.Frame(); err != nil {
		t.Fatal(err)
	}
	if err := s.ClickLabel("button:AI 生成提交说明"); err != nil {
		t.Fatal(err)
	}
	if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.commitAI.request == nil }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if m.tools.git.commitAI.problem == "" || m.tools.git.commit.Text() != "keep my draft" {
		t.Fatal("a failed generation erased the draft or hid its error")
	}
}

func TestGitCommitAICancelsInFlightRequests(t *testing.T) {
	for _, reason := range []string{"cancel", "selection", "close", "repository"} {
		t.Run(reason, func(t *testing.T) {
			_, m, s, _ := gitCommitAIUIFixture(t)
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				close(started)
				select {
				case <-r.Context().Done():
					close(canceled)
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			profile := aiSettings{ID: 1, Name: "可取消服务", Provider: "custom", BaseURL: server.URL, Model: "test", Protocol: aiChat, Auth: aiAuthNone}
			check((aiProfiles{Profiles: []aiSettings{profile}}).save(m.dataDirectory))
			m.tools.git.commit.SetText("keep my draft")
			check(s.Frame())
			check(s.ClickLabel("button:AI 生成提交说明"))
			check(s.WaitFor(func(app.Snapshot) bool {
				select {
				case <-started:
					return true
				default:
					return false
				}
			}, 10*time.Second))
			// The generation must not enable a commit before the text is ready.
			check(s.ClickLabel("button:提交"))
			if m.tools.git.request != nil {
				t.Fatal("commit was started while AI was still generating")
			}
			switch reason {
			case "cancel":
				check(s.ClickLabel("button:取消生成提交说明"))
			case "selection":
				check(s.ClickLabel("checkbox:选择提交 · selected.txt"))
			case "close":
				check(s.ClickLabel("button:关闭 Git 侧栏"))
			case "repository":
				other, _ := gitReviewFixture(t)
				m.openProjectGit(other)
				check(s.Frame())
			}
			check(s.WaitFor(func(app.Snapshot) bool {
				select {
				case <-canceled:
					return true
				default:
					return false
				}
			}, 5*time.Second))
			if m.tools.git.commitAI.request != nil {
				t.Fatal("the canceled generation remained active")
			}
			if reason != "repository" && m.tools.git.commit.Text() != "keep my draft" {
				t.Fatal("canceling generation changed the handwritten draft")
			}
		})
	}
}

func TestGitCommitAIProfileSelection(t *testing.T) {
	t.Run("unconfigured", func(t *testing.T) {
		_, m, s, _ := gitCommitAIUIFixture(t)
		if err := s.ClickLabel("button:AI 生成提交说明"); err != nil {
			t.Fatal(err)
		}
		if m.tools.git.commitAI.request != nil {
			t.Fatal("generation started without a profile")
		}
		if err := s.ClickLabel("menuitem:打开 AI 设置…"); err != nil {
			t.Fatal(err)
		}
		if !m.appSettings.open || m.appSettings.section != settingsAI {
			t.Fatal("unconfigured generation did not offer AI settings")
		}
	})
	t.Run("multiple profiles", func(t *testing.T) {
		_, m, s, _ := gitCommitAIUIFixture(t)
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			var request struct{ Model string }
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "chosen-model" {
				t.Error("wrong profile was used", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"chosen profile result"},"finish_reason":"stop"}]}`)
		}))
		defer server.Close()
		profiles := []aiSettings{
			{ID: 1, Name: "默认服务", Provider: "custom", BaseURL: server.URL, Model: "unused-model", Protocol: aiChat, Auth: aiAuthNone},
			{ID: 2, Name: "所选服务", Provider: "custom", BaseURL: server.URL, Model: "chosen-model", Protocol: aiChat, Auth: aiAuthNone},
		}
		if err := (aiProfiles{DefaultID: 1, Profiles: profiles}).save(m.dataDirectory); err != nil {
			t.Fatal(err)
		}
		if err := s.ClickLabel("button:AI 生成提交说明"); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 0 || m.tools.git.commitAI.request != nil {
			t.Fatal("generation used the default profile before choosing a service")
		}
		if err := s.ClickLabel("menuitem:" + aiServiceModelLabel(profiles[1])); err != nil {
			t.Fatal(err)
		}
		if err := s.WaitFor(func(app.Snapshot) bool { return m.tools.git.commitAI.request == nil }, 10*time.Second); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 1 || m.tools.git.commit.Text() != "chosen profile result" {
			t.Fatal("the selected profile did not generate the message")
		}
	})
}

func TestGitCommitAIWaitsForStatusRefresh(t *testing.T) {
	_, m, s, _ := gitCommitAIUIFixture(t)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	started, releaseReply, releaseRefresh := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			return
		case <-releaseReply:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"stale branch message"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-releaseReply:
		default:
			close(releaseReply)
		}
		select {
		case <-releaseRefresh:
		default:
			close(releaseRefresh)
		}
	}()
	profile := aiSettings{ID: 1, Name: "刷新测试", Provider: "custom", BaseURL: server.URL, Model: "test", Protocol: aiChat, Auth: aiAuthNone}
	check((aiProfiles{Profiles: []aiSettings{profile}}).save(m.dataDirectory))
	m.tools.git.commit.SetText("keep my draft")
	check(s.Frame())
	check(s.ClickLabel("button:AI 生成提交说明"))
	check(s.WaitFor(func(app.Snapshot) bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, 10*time.Second))
	status := m.tools.git.snapshot
	status.branch = "changed-outside-editor"
	m.tools.git.requestAction = gitRefresh
	m.tools.git.request = ui.NewEffect[gitResult, int](func(ctx context.Context) (gitResult, error) {
		select {
		case <-ctx.Done():
			return gitResult{}, ctx.Err()
		case <-releaseRefresh:
			return gitResult{action: gitRefresh, status: status, statusValid: true}, nil
		}
	})
	check(s.Frame())
	close(releaseReply)
	// Read the completed effect only from the UI test thread. If the UI consumes
	// it before processing the refresh, it clears request and this fails.
	check(s.WaitFor(func(app.Snapshot) bool {
		request := m.tools.git.commitAI.request
		if request == nil || m.tools.git.commit.Text() != "keep my draft" {
			t.Fatal("AI applied its reply before the pending Git refresh")
		}
		output, err, done := request.Take()
		if done && (err != nil || output != "stale branch message") {
			t.Fatalf("unexpected completed AI response: %q, %v", output, err)
		}
		return done
	}, 10*time.Second))
	close(releaseRefresh)
	check(s.WaitFor(func(app.Snapshot) bool {
		return m.tools.git.request == nil && m.tools.git.commitAI.request == nil
	}, 10*time.Second))
	if m.tools.git.snapshot.branch != status.branch || m.tools.git.commit.Text() != "keep my draft" || m.tools.git.commitAI.problem == "" {
		t.Fatal("changed branch did not invalidate the old generation")
	}
}
