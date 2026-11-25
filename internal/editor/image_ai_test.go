package editor

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
)

func TestImageAIOnlySendsCurrentImageOnStart(t *testing.T) {
	for _, protocol := range []aiProtocol{aiChat, aiResponses, aiMessages} {
		t.Run(string(protocol), func(t *testing.T) {
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index := calls.Add(1)
				if r.URL.Path != "/v1/"+string(protocol) || r.Method != http.MethodPost ||
					r.Header.Get("X-Image-Test") != "profile-header" {
					t.Error("request did not use the selected endpoint or custom profile headers")
				}
				if protocol == aiMessages {
					if r.Header.Get("X-Api-Key") != "vision-test-token" {
						t.Error("Anthropic request did not use the configured authentication")
					}
				} else if r.Header.Get("Authorization") != "Bearer vision-test-token" {
					t.Error("OpenAI-style request did not use the configured authentication")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				var request struct {
					Model    string                   `json:"model"`
					Store    bool                     `json:"store"`
					Messages []imageAIReceivedMessage `json:"messages"`
					Input    []imageAIReceivedMessage `json:"input"`
				}
				if err := json.Unmarshal(body, &request); err != nil || request.Model != "selected-vision-model" ||
					request.Store || bytes.Contains(body, []byte("unrelated-document-sentinel")) || bytes.Contains(body, []byte("current-file-name.png")) {
					t.Error("request did not use the selected model and only the current image")
				}
				messages := request.Messages
				promptType, imageType := "text", "image_url"
				result := "{\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"测试识图结果\"},\"finish_reason\":\"stop\"}]}"
				if protocol == aiResponses {
					messages, promptType, imageType = request.Input, "input_text", "input_image"
					result = "{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"测试识图结果\"}]}]}"
				} else if protocol == aiMessages {
					imageType = "image"
					result = "{\"stop_reason\":\"end_turn\",\"content\":[{\"type\":\"text\",\"text\":\"测试识图结果\"}]}"
				}
				if len(messages) != 1 || messages[0].Role != "user" || len(messages[0].Content) != 2 {
					t.Error("expected exactly one user message containing its instructions and current image")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				prompt, picture := messages[0].Content[0], messages[0].Content[1]
				if protocol == aiMessages {
					prompt, picture = picture, prompt
				}
				wantPrompt := "抄录当前图片"
				if index == 2 {
					wantPrompt = "解读当前图片"
				}
				if prompt.Type != promptType || !strings.Contains(prompt.Text, wantPrompt) || picture.Type != imageType {
					t.Error("unexpected image content blocks or mode prompt")
				}
				data := ""
				switch protocol {
				case aiChat:
					var url aiVisionChatURL
					if err := json.Unmarshal(picture.ImageURL, &url); err != nil {
						t.Error(err)
					}
					data, _ = strings.CutPrefix(url.URL, "data:image/png;base64,")
				case aiResponses:
					var url string
					if err := json.Unmarshal(picture.ImageURL, &url); err != nil {
						t.Error(err)
					}
					data, _ = strings.CutPrefix(url, "data:image/png;base64,")
				case aiMessages:
					if picture.Source.Type != "base64" || picture.Source.MediaType != "image/png" {
						t.Error("unexpected Anthropic image encoding")
					}
					data = picture.Source.Data
				}
				raw, err := base64.StdEncoding.DecodeString(data)
				if err != nil {
					t.Error(err)
				}
				uploaded, err := png.Decode(bytes.NewReader(raw))
				if err != nil || uploaded.Bounds().Dx() != 2 || uploaded.Bounds().Dy() != 1 ||
					color.NRGBAModel.Convert(uploaded.At(0, 0)) != (color.NRGBA{R: 240, G: 42, B: 5, A: 255}) ||
					color.NRGBAModel.Convert(uploaded.At(1, 0)) != (color.NRGBA{}) {
					t.Error("uploaded bytes did not contain the current preview pixels", err)
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, result)
			}))
			defer server.Close()

			m := newEditorApp()
			m.dataDirectory = t.TempDir()
			spec := aiSettings{ID: 2, Name: "视觉服务", Provider: "custom", BaseURL: server.URL, Model: "selected-vision-model", Protocol: protocol}
			var err error
			spec.Key, err = windowsAIKey([]byte("vision-test-token"), true)
			must(err)
			spec.Headers, err = windowsAIKey([]byte("{\"X-Image-Test\":\"profile-header\"}"), true)
			must(err)
			profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{
				{ID: 1, Name: "默认服务", Provider: "custom", BaseURL: server.URL, Model: "unused-model", Protocol: protocol},
				spec,
				{ID: 3, Name: "CLI 服务", Provider: aiCodexCLI, Model: "unused-cli"},
			}}
			must(profiles.save(m.dataDirectory))
			m.addTab(document{saved: "unrelated-document-sentinel"}, "unrelated-document-sentinel.txt")
			picture := image.NewNRGBA(image.Rect(0, 0, 2, 1))
			picture.SetNRGBA(0, 0, color.NRGBA{R: 240, G: 42, B: 5, A: 255})
			var original bytes.Buffer
			must(png.Encode(&original, picture))
			path := filepath.Join(m.dataDirectory, "current-file-name.png")
			must(os.WriteFile(path, original.Bytes(), 0600))
			m.load(path)
			// The disk file may change after it was opened. The request must follow the displayed preview.
			must(os.WriteFile(path, []byte("unrelated-document-sentinel"), 0600))
			session := openTestSession(t, m)
			must(session.ClickLabel("button:AI 识图"))
			state := m.imageAI
			if state == nil || state.modelID != 1 || state.request != nil || calls.Load() != 0 {
				t.Fatal("opening the dialog unexpectedly started a request or chose the wrong default")
			}
			must(session.ClickLabel("combobox:识图模型"))
			if _, err := session.Locate("option:CLI 服务 · unused-cli"); err == nil {
				t.Fatal("a text-only CLI adapter was offered")
			}
			must(session.ClickLabel("option:视觉服务 · selected-vision-model"))
			if state.modelID != 2 || state.request != nil || calls.Load() != 0 {
				t.Fatal("switching the model unexpectedly started a request")
			}

			for count, mode := range []string{"提取文字", "解读内容"} {
				must(session.ClickLabel("button:" + mode))
				if state.request != nil || calls.Load() != int32(count) {
					t.Fatal("switching modes sent the image")
				}
				must(session.ClickLabel("button:开始识图"))
				must(session.WaitFor(func(app.Snapshot) bool { return state.request == nil }, 3*time.Second))
				if state.problem != "" || state.output.Text() != "测试识图结果" || calls.Load() != int32(count+1) {
					t.Fatal("AI result did not reach the dialog", state.problem, state.output.Text(), calls.Load())
				}
			}
			must(session.ClickLabel("button:复制全文"))
			if session.Driver().Clipboard() != "测试识图结果" {
				t.Fatal("could not copy the image result")
			}
			if _, err := os.Stat(filepath.Join(m.dataDirectory, imageOCRDirectory)); !os.IsNotExist(err) {
				t.Fatal("AI recognition installed local OCR components", err)
			}
		})
	}
}

type imageAIReceivedMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type     string               `json:"type"`
		Text     string               `json:"text"`
		ImageURL json.RawMessage      `json:"image_url"`
		Source   aiVisionClaudeSource `json:"source"`
	} `json:"content"`
}

func TestImageAIWithoutConfiguredAPI(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load("assets/icon.png")
	session := openTestSession(t, m)
	if err := session.ClickLabel("button:AI 识图"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Locate("button:开始识图"); err == nil {
		t.Fatal("image upload was enabled without an API profile")
	}
	if err := session.ClickLabel("button:打开 AI 设置"); err != nil || m.imageAI != nil || m.appSettings.section != settingsAI {
		t.Fatal("could not open AI settings from the image dialog", err)
	}
}

func TestImageAICancelsAnInFlightRequest(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.load("assets/icon.png")
	profiles := aiProfiles{DefaultID: 1, Profiles: []aiSettings{
		{ID: 1, Name: "视觉服务", Provider: "custom", BaseURL: server.URL, Model: "model", Protocol: aiChat},
	}}
	if err := profiles.save(m.dataDirectory); err != nil {
		t.Fatal(err)
	}
	session := openTestSession(t, m)
	for _, label := range []string{"AI 识图", "开始识图"} {
		if err := session.ClickLabel("button:" + label); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.WaitFor(func(app.Snapshot) bool {
		select {
		case <-started:
			return true
		default:
			return false
		}
	}, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := session.ClickLabel("button:取消"); err != nil {
		t.Fatal(err)
	}
	if err := session.WaitFor(func(app.Snapshot) bool {
		select {
		case <-canceled:
			return true
		default:
			return false
		}
	}, 3*time.Second); err != nil {
		t.Fatal("request continued after cancel", err)
	}
	if m.imageAI.request != nil || m.imageAI.output.Text() != "" {
		t.Fatal("a canceled request remained visible")
	}
}
