package editor

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Only this small worker script is embedded, never its runtime or language models.
//
//go:embed offline_translation_worker.py
var offlineTranslationWorker string

type offlineTextPart struct {
	Text string `json:"text"`
	Keep bool   `json:"keep"`
}

type offlineTranslationRequest struct {
	Model  string            `json:"model"`
	Target string            `json:"target"`
	Parts  []offlineTextPart `json:"parts"`
}

type offlineTranslationResult struct {
	Text           string  `json:"text"`
	LoadMS         float64 `json:"loadMs"`
	TranslateMS    float64 `json:"translateMs"`
	PeakWorkingSet uint64  `json:"peakWorkingSet"`
	PrivateBytes   uint64  `json:"privateBytes"`
	ElapsedMS      float64 `json:"elapsedMs"`
}

func translateOffline(ctx context.Context, directory, source, target, input string) (offlineTranslationResult, error) {
	var result offlineTranslationResult
	model := offlineLanguageModel(source, target)
	if model == nil {
		return result, errors.New("离线整句暂支持英语与简体中文互译")
	}
	if strings.TrimSpace(input) == "" || len(input) > translationTextLimit {
		return result, errors.New("请输入 8 KiB 以内的原文")
	}
	for _, component := range []offlineComponent{offlineRuntime, *model} {
		info, err := readOfflineInstallation(directory, component)
		if err != nil {
			return result, err
		}
		if info == nil {
			return result, errors.New("请先下载离线运行时和当前语言包")
		}
	}
	request := offlineTranslationRequest{Model: offlineComponentPath(directory, *model), Target: target}
	start := 0
	for _, span := range translationMarkerPattern().FindAllStringIndex(input, -1) {
		if start < span[0] {
			request.Parts = append(request.Parts, offlineTextPart{Text: input[start:span[0]]})
		}
		request.Parts = append(request.Parts, offlineTextPart{Text: input[span[0]:span[1]], Keep: true})
		start = span[1]
	}
	if start < len(input) {
		request.Parts = append(request.Parts, offlineTextPart{Text: input[start:]})
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	runtime := offlineComponentPath(directory, offlineRuntime)
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(runtime, "python.exe"), "-I", "-B", "-c", offlineTranslationWorker)
	command.Dir = runtime
	// MKL pre-packing committed ~908 MiB for this small model in Windows tests.
	// The alternative CPU backend used ~125 MiB with comparable short-text latency.
	command.Env = append(command.Environ(), "OMP_NUM_THREADS=2", "OPENBLAS_NUM_THREADS=1", "CT2_VERBOSE=0", "CT2_USE_MKL=0")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00004000}
	command.WaitDelay = 2 * time.Second
	command.Stdin = bytes.NewReader(payload)
	stdout, stderr := aiCLICapture{limit: 64 << 10}, aiCLICapture{limit: 8192}
	command.Stdout, command.Stderr = &stdout, &stderr
	started := time.Now()
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if index := strings.LastIndexByte(detail, '\n'); index >= 0 {
			detail = detail[index+1:]
		}
		return result, fmt.Errorf("离线引擎运行失败：%s（%w）", detail, err)
	}
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		return result, errors.New("离线引擎返回了无效结果")
	}
	result.ElapsedMS = float64(time.Since(started).Microseconds()) / 1000
	if strings.TrimSpace(result.Text) == "" || len(result.Text) > translationTextLimit || strings.ContainsRune(result.Text, 0) {
		return result, errors.New("离线译文为空或超过 8 KiB，请缩小原文范围")
	}
	if err := validateTranslationMarkers(input, result.Text); err != nil {
		return result, err
	}
	// Run waits for process exit: no interpreter, model or worker remains resident.
	return result, nil
}
