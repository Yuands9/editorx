package editor

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/vivaui/viva/gfx"
)

const (
	aiImageMaxSide  = 2400
	aiImageMaxBytes = 3 << 20
)

var errAIImageTooLarge = errors.New("图片上传数据超过 3 MiB，请缩小图片后重试")

type aiImageBuffer struct {
	bytes.Buffer
	ctx context.Context
}

func (buffer *aiImageBuffer) Write(data []byte) (int, error) {
	if err := buffer.ctx.Err(); err != nil {
		return 0, err
	}
	if len(data) > aiImageMaxBytes-buffer.Len() {
		return 0, errAIImageTooLarge
	}
	return buffer.Buffer.Write(data)
}

func aiImageUpload(ctx context.Context, bitmap gfx.Bitmap) (mediaType, data string, err error) {
	if !validImageBitmap(bitmap) {
		return "", "", errors.New("图片像素数据不完整")
	}

	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	width, height := bitmap.Width, bitmap.Height
	if longest := max(width, height); longest > aiImageMaxSide {
		width = max(1, width*aiImageMaxSide/longest)
		height = max(1, height*aiImageMaxSide/longest)
	}
	upload := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return "", "", err
		}
		source := bitmap.Pix[y*bitmap.Height/height*bitmap.Stride:]
		row := upload.Pix[y*upload.Stride:][:width*4]
		for x := 0; x < width; x++ {
			offset := x * bitmap.Width / width * 4
			blue, green, red, alpha := source[offset], source[offset+1], source[offset+2], source[offset+3]
			// Keep the preview's premultiplied alpha while swapping BGRA to RGBA.
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = red, green, blue, alpha
		}
	}

	buffer := aiImageBuffer{ctx: ctx}
	mediaType = "image/png"
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	err = encoder.Encode(&buffer, upload)
	if errors.Is(err, errAIImageTooLarge) {
		mediaType = "image/jpeg"
		// JPEG has no alpha channel. Composite onto white only for this fallback.
		for offset := 0; offset < len(upload.Pix); offset += 4 {
			white := 255 - upload.Pix[offset+3]
			upload.Pix[offset] += white
			upload.Pix[offset+1] += white
			upload.Pix[offset+2] += white
			upload.Pix[offset+3] = 255
		}
		buffer.Reset()
		err = jpeg.Encode(&buffer, upload, &jpeg.Options{Quality: 90})
	}
	if err != nil {
		return "", "", err
	}
	return mediaType, base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

type aiVisionMessage[Block any] struct {
	Role    string  `json:"role"`
	Content []Block `json:"content"`
}

type aiVisionChatURL struct {
	URL string `json:"url"`
}

type aiVisionChatBlock struct {
	Type     string           `json:"type"`
	Text     string           `json:"text,omitempty"`
	ImageURL *aiVisionChatURL `json:"image_url,omitempty"`
}

type aiVisionResponsesBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type aiVisionClaudeSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type aiVisionClaudeBlock struct {
	Type   string                `json:"type"`
	Text   string                `json:"text,omitempty"`
	Source *aiVisionClaudeSource `json:"source,omitempty"`
}

func (settings aiSettings) recognizeImageWithAI(ctx context.Context, prompt string, bitmap gfx.Bitmap) (string, error) {
	if settings.usesCLI() || !validAIModelID(settings.Model) {
		return "", errors.New("请选择支持图片输入的 API 模型")
	}
	protocol := settings.Protocol
	if protocol == "" {
		protocol = aiChat
	}
	mediaType, data, err := aiImageUpload(ctx, bitmap)
	if err != nil {
		return "", err
	}

	var payload any
	switch protocol {
	case aiChat:
		payload = struct {
			Model    string                               `json:"model"`
			Messages []aiVisionMessage[aiVisionChatBlock] `json:"messages"`
		}{
			Model: settings.Model,
			Messages: []aiVisionMessage[aiVisionChatBlock]{{Role: "user", Content: []aiVisionChatBlock{
				{Type: "text", Text: prompt},
				{Type: "image_url", ImageURL: &aiVisionChatURL{URL: "data:" + mediaType + ";base64," + data}},
			}}},
		}
	case aiResponses:
		payload = struct {
			Model string                                    `json:"model"`
			Input []aiVisionMessage[aiVisionResponsesBlock] `json:"input"`
			Store bool                                      `json:"store"`
		}{
			Model: settings.Model,
			Input: []aiVisionMessage[aiVisionResponsesBlock]{{Role: "user", Content: []aiVisionResponsesBlock{
				{Type: "input_text", Text: prompt},
				{Type: "input_image", ImageURL: "data:" + mediaType + ";base64," + data, Detail: "high"},
			}}},
		}
	case aiMessages:
		payload = struct {
			Model     string                                 `json:"model"`
			Messages  []aiVisionMessage[aiVisionClaudeBlock] `json:"messages"`
			MaxTokens int                                    `json:"max_tokens"`
		}{
			Model: settings.Model,
			Messages: []aiVisionMessage[aiVisionClaudeBlock]{{Role: "user", Content: []aiVisionClaudeBlock{
				{Type: "image", Source: &aiVisionClaudeSource{Type: "base64", MediaType: mediaType, Data: data}},
				{Type: "text", Text: prompt},
			}}},
			MaxTokens: 8192,
		}
	default:
		return "", errors.New("无法识别此配置的 API 协议")
	}

	reply, err := settings.requestAICompletion(ctx, protocol, payload, nil)
	if err != nil {
		var responseErr aiHTTPError
		if errors.As(err, &responseErr) {
			switch responseErr.status {
			case 400, 415, 422:
				return "", fmt.Errorf("%w。请确认模型和接口支持图片输入", err)
			}
		}
		return "", err
	}
	if reply.Truncated {
		return "", errors.New("识图结果未完整返回，请尝试较小的图片")
	}
	result := strings.Trim(reply.Text, "\r\n")
	if strings.TrimSpace(result) == "" || strings.ContainsRune(result, 0) {
		return "", errors.New("模型未返回可用的识图结果")
	}
	return result, nil
}
