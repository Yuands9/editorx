package editor

import (
	"strings"
	"testing"
)

func TestAIStreamKeepsTextAndExcludesThinking(t *testing.T) {
	for _, test := range []struct {
		protocol aiProtocol
		stream   string
	}{
		{aiChat, `data: {"choices":[{"delta":{"reasoning_content":"隐藏"}}]}

data: {"choices":[{"delta":{"content":"你好"}}]}

data: {"choices":[{"delta":{"content":"，世界"},"finish_reason":"stop"}]}

data: [DONE]

`},
		{aiResponses, `data: {"type":"response.output_text.delta","delta":"你好"}

data: {"type":"response.output_text.delta","delta":"，世界"}

data: {"type":"response.completed","response":{"output":[],"status":"completed"}}

`},
		{aiMessages, `data: {"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"隐藏"}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"你好"}}

data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"，世界"}}

data: {"type":"message_stop"}

`},
	} {
		t.Run(string(test.protocol), func(t *testing.T) {
			var deltas []string
			reply, err := readAIStream(test.protocol, strings.NewReader(test.stream), func(delta string) {
				deltas = append(deltas, delta)
			})
			if err != nil || len(deltas) != 2 || reply.Text != "你好，世界" || reply.Truncated {
				t.Fatalf("stream delivered %q and %+v: %v", deltas, reply, err)
			}
		})
	}
}

func TestStreamedReplySwitchesToMarkdownWithoutChangingText(t *testing.T) {
	var body aiMessageBody
	value := "```json\n{\"ready\": true}\n```"
	body.update(value, 14, false)
	if body.markdown != nil {
		t.Fatal("unfinished reply was formatted")
	}
	body.update(value, 14, true)
	if body.markdown == nil || len(body.markdown.document.Blocks) == 0 {
		t.Fatal("completed reply did not switch to Markdown")
	}
}

func TestSavedAIKeyVisibilityDoesNotReplaceCiphertext(t *testing.T) {
	const key = "key-for-local-test"
	ciphertext, err := windowsAIKey([]byte(key), true)
	if err != nil {
		t.Fatal(err)
	}
	var editor aiSettingsEditor
	editor.load(aiSettings{Name: "DeepSeek", Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1", Protocol: aiChat, Model: "deepseek-test", Key: ciphertext})
	if editor.key.Text() != "" || !editor.key.Secure {
		t.Fatal("a saved key was shown initially")
	}
	editor.toggleKeyVisibility()
	if editor.key.Secure || editor.key.Text() != key || editor.dirty() {
		t.Fatal("revealing the saved key changed the profile")
	}
	editor.toggleKeyVisibility()
	if editor.key.Text() != "" || !editor.key.Secure {
		t.Fatal("hiding the saved key left it in the field")
	}
}
