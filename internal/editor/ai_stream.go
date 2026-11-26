package editor

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

type aiReplyStream struct {
	mu    sync.Mutex
	value strings.Builder
	paint time.Time
}

func (stream *aiReplyStream) append(delta string) bool {
	if delta == "" {
		return false
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.value.WriteString(delta)
	if time.Since(stream.paint) < 40*time.Millisecond {
		return false
	}
	stream.paint = time.Now()
	return true
}

func (stream *aiReplyStream) text() string {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return stream.value.String()
}

func (stream *aiReplyStream) replace(value string) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	stream.value.Reset()
	stream.value.WriteString(value)
}

type aiSSEEvent struct {
	Type         string          `json:"type"`
	Model        string          `json:"model"`
	ContentBlock aiTextBlock     `json:"content_block"`
	Delta        json.RawMessage `json:"delta"`
	Error        json.RawMessage `json:"error"`
	Response     json.RawMessage `json:"response"`
	Message      struct {
		Model string `json:"model"`
	} `json:"message"`
	Choices []struct {
		Delta        aiMessage `json:"delta"`
		FinishReason string    `json:"finish_reason"`
	} `json:"choices"`
}

func readAIStream(protocol aiProtocol, reader io.Reader, onDelta func(string)) (aiCompletion, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 8192), aiResponseLimit+8192)
	var reply aiCompletion
	var output, eventData strings.Builder
	var done bool
	readEvent := func() error {
		payload := eventData.String()
		eventData.Reset()
		if strings.TrimSpace(payload) == "[DONE]" {
			done = true
			return nil
		}
		var event aiSSEEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			return errors.New("模型返回了无效的流式事件")
		}
		if len(event.Error) > 0 && string(event.Error) != "null" || event.Type == "error" || event.Type == "response.failed" {
			return errors.New("服务商中断了流式回复")
		}
		if event.Model != "" {
			reply.Model = event.Model
		}
		var delta string
		switch protocol {
		case aiChat:
			if len(event.Choices) != 0 {
				choice := event.Choices[0]
				delta = choice.Delta.Content
				if choice.FinishReason != "" {
					done, reply.Truncated = true, choice.FinishReason == "length"
				}
			}
		case aiResponses:
			switch event.Type {
			case "response.output_text.delta":
				if err := json.Unmarshal(event.Delta, &delta); err != nil {
					return errors.New("Responses 流式回复中的文本格式无效")
				}
			case "response.completed", "response.incomplete":
				done, reply.Truncated = true, event.Type == "response.incomplete"
				if complete, err := decodeAICompletion(protocol, event.Response); err == nil {
					reply.Model = complete.Model
					if output.Len() == 0 {
						delta = complete.Text
					}
				}
			}
		case aiMessages:
			switch event.Type {
			case "message_start":
				reply.Model = event.Message.Model
			case "content_block_start":
				if event.ContentBlock.Type == "text" {
					delta = event.ContentBlock.Text
				}
			case "content_block_delta", "message_delta":
				var block struct {
					Type, Text string
					StopReason string `json:"stop_reason"`
				}
				if err := json.Unmarshal(event.Delta, &block); err != nil {
					return errors.New("Messages 流式回复中的文本格式无效")
				}
				if block.Type == "text_delta" {
					delta = block.Text
				}
				if block.StopReason == "max_tokens" {
					reply.Truncated = true
				}
			case "message_stop":
				done = true
			}
		}
		if output.Len()+len(delta) > aiResponseLimit {
			return errors.New("模型响应超过 2 MB")
		}
		if delta != "" {
			output.WriteString(delta)
			onDelta(delta)
		}
		return nil
	}
	for !done && scanner.Scan() {
		line := scanner.Text()
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			if eventData.Len()+len(value) > aiResponseLimit+4096 {
				return aiCompletion{}, errors.New("流式事件超过 2 MB")
			}
			if eventData.Len() != 0 {
				eventData.WriteByte('\n')
			}
			eventData.WriteString(strings.TrimPrefix(value, " "))
			continue
		}
		if line == "" && eventData.Len() != 0 {
			if err := readEvent(); err != nil {
				return aiCompletion{}, err
			}
		}
	}
	if !done && eventData.Len() != 0 && scanner.Err() == nil {
		if err := readEvent(); err != nil {
			return aiCompletion{}, err
		}
	}
	if scanner.Err() != nil || !done {
		return aiCompletion{}, errors.New("连接中断，未收到完整的流式回复")
	}
	reply.Text = output.String()
	return reply, nil
}
