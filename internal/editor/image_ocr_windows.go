package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
)

var imageOCRComponentLock sync.Mutex

const imageOCRWorker = `
import sys
from tesserocr import OEM, PSM, PyTessBaseAPI

width, height = map(int, sys.argv[1:3])
pixels = sys.stdin.buffer.read()
if len(pixels) != width * height:
    raise ValueError("Incomplete image")
with PyTessBaseAPI(path="tessdata", lang="chi_sim+eng", oem=OEM.LSTM_ONLY, psm=PSM.AUTO) as engine:
    engine.SetImageBytes(pixels, width, height, 1, width)
    engine.SetSourceResolution(160)
    sys.stdout.buffer.write(engine.GetUTF8Text().encode("utf-8"))
`

func recognizeImageText(ctx context.Context, directory string, bitmap gfx.Bitmap) (string, error) {
	pixels, width, height, err := imageOCRPixels(ctx, bitmap)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	path := offlineComponentPath(directory, imageOCRComponent)
	command := exec.CommandContext(ctx, filepath.Join(path, "python.exe"), "-I", "-B", "-c", imageOCRWorker, strconv.Itoa(width), strconv.Itoa(height))
	command.Dir = path
	command.Env = append(command.Environ(), "OMP_THREAD_LIMIT=2")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00004000}
	command.WaitDelay = 2 * time.Second
	command.Stdin = bytes.NewReader(pixels)
	stdout, stderr := aiCLICapture{limit: 256 << 10}, aiCLICapture{limit: 8192}
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		detail := strings.TrimSpace(stderr.String())
		if index := strings.LastIndexByte(detail, '\n'); index >= 0 {
			detail = strings.TrimSpace(detail[index+1:])
		}
		if detail != "" {
			return "", fmt.Errorf(uiText("OCR 引擎运行失败：%s"), detail)
		}
		return "", fmt.Errorf(uiText("OCR 引擎运行失败：%w"), err)
	}

	// Run waits until Python, Tesseract and the models have left memory.
	output := strings.TrimSpace(stdout.String())
	if stdout.overflow || !utf8.ValidString(output) || strings.ContainsRune(output, 0) {
		return "", errors.New(uiText("识别结果过长或无效，请尝试较小的图片"))
	}
	return output, nil
}

func imageOCRPixels(ctx context.Context, bitmap gfx.Bitmap) ([]byte, int, int, error) {
	const maxSide = 3500
	if !validImageBitmap(bitmap) {
		return nil, 0, 0, errors.New(uiText("图片像素数据不完整"))
	}

	width, height := bitmap.Width, bitmap.Height
	if longest := max(width, height); longest > maxSide {
		width = max(1, width*maxSide/longest)
		height = max(1, height*maxSide/longest)
	}
	pixels := make([]byte, width*height)
	for y := 0; y < height; y++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, 0, err
		}
		row := bitmap.Pix[(y*bitmap.Height/height)*bitmap.Stride:]
		for x := 0; x < width; x++ {
			offset := (x * bitmap.Width / width) * 4
			blue, green, red, alpha := int(row[offset]), int(row[offset+1]), int(row[offset+2]), int(row[offset+3])
			// Preview pixels are premultiplied BGRA. Composite transparency against white.
			gray := ((29*blue + 150*green + 77*red + 128) >> 8) + 255 - alpha
			pixels[y*width+x] = byte(min(gray, 255))
		}
	}
	return pixels, width, height, nil
}
