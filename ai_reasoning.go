package main

import (
	"net/url"
	"slices"
	"strings"
)

type aiReasoning string

const (
	aiReasoningLow    aiReasoning = "low"
	aiReasoningMedium aiReasoning = "medium"
	aiReasoningHigh   aiReasoning = "high"
	aiReasoningMax    aiReasoning = "max"
)

type aiEffortConfig struct {
	Effort aiReasoning `json:"effort"`
}

type aiThinkingConfig struct {
	Type string `json:"type"`
}

var generalReasoningLevels = []aiReasoning{"", aiReasoningLow, aiReasoningMedium, aiReasoningHigh}
var deepSeekReasoningLevels = []aiReasoning{"", aiReasoningLow, aiReasoningHigh, aiReasoningMax}

func (settings aiSettings) isDeepSeek() bool {
	if settings.Provider == "deepseek" {
		return true
	}
	address, err := url.Parse(settings.BaseURL)
	return err == nil && strings.EqualFold(address.Hostname(), "api.deepseek.com")
}

func (settings aiSettings) reasoningLevels() []aiReasoning {
	if settings.isDeepSeek() {
		return deepSeekReasoningLevels
	}
	return generalReasoningLevels
}

func (settings aiSettings) effectiveReasoning(level aiReasoning) aiReasoning {
	if slices.Contains(settings.reasoningLevels(), level) {
		return level
	}
	return ""
}

func aiReasoningLabel(effort aiReasoning) string {
	switch effort {
	case aiReasoningLow:
		return "低"
	case aiReasoningMedium:
		return "中"
	case aiReasoningHigh:
		return "高"
	case aiReasoningMax:
		return "最高"
	default:
		return "服务默认"
	}
}
