package editor

import (
	"embed"
	"image/png"
	"sync"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

//go:embed assets/ai-providers/*.png
var aiLogoFiles embed.FS

var aiProviderLogos = map[string]string{
	"openai": "openai", "anthropic": "claude", "deepseek": "deepseek",
	"qwen": "qwen", "gemini": "gemini", "minimax": "minimax", "ollama": "ollama", "zhipu": "zhipu",
	"compatible": "openai", "anthropic-compatible": "claude",
	aiCodexCLI: "codex", aiClaudeCLI: "claudecode", aiCodeBuddyCLI: "codebuddy",
	aiCursorCLI: "cursor", aiGrokCLI: "grok", aiOpenCodeCLI: "opencode", aiPiCLI: "pi",
}

var aiLogoBitmaps = sync.OnceValue(func() map[string]gfx.Bitmap {
	images := make(map[string]gfx.Bitmap)
	files, _ := aiLogoFiles.ReadDir("assets/ai-providers")
	for _, file := range files {
		source, err := aiLogoFiles.Open("assets/ai-providers/" + file.Name())
		if err != nil {
			continue
		}
		picture, err := png.Decode(source)
		source.Close()
		if err == nil {
			images[file.Name()] = imageToVivaBitmap(picture)
		}
	}
	return images
})

var aiIcon = gfx.Icon{ViewBox: 24, Stroke: 1.55, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(4.5, 15.75), gfx.LineTo(4.5, 8.5), gfx.QuadTo(4.5, 6.5, 6.5, 6.5),
	gfx.LineTo(17.5, 6.5), gfx.QuadTo(19.5, 6.5, 19.5, 8.5),
	gfx.LineTo(19.5, 15.75), gfx.QuadTo(19.5, 17.75, 17.5, 17.75),
	gfx.LineTo(6.5, 17.75), gfx.QuadTo(4.5, 17.75, 4.5, 15.75), gfx.ClosePath(),
	gfx.MoveTo(8, 2.5), gfx.LineTo(12, 2.5), gfx.LineTo(12, 6.5),
	gfx.MoveTo(2.5, 12.25), gfx.LineTo(4.5, 12.25),
	gfx.MoveTo(19.5, 12.25), gfx.LineTo(21.5, 12.25),
	gfx.MoveTo(9.3, 11.4), gfx.LineTo(9.3, 13.1),
	gfx.MoveTo(14.7, 11.4), gfx.LineTo(14.7, 13.1),
}}

func (m *editorApp) aiAssistantMark(c ui.C, size float32) {
	ui.Row(c).Size(size, size).Shrink(0).Center().Children(func() {
		ui.Icon(c, aiIcon).Size(size*.77, size*.77).Color(m.theme.MutedForeground)
	})
}

func (m *editorApp) aiProviderMark(c ui.C, provider string, size float32) {
	stem := aiProviderLogos[provider]
	if provider == "kimi" || provider == aiQoderCLI {
		letter, color := "K", m.theme.Foreground
		if provider == aiQoderCLI {
			letter = "Q"
		}
		ui.Row(c).Size(size, size).Shrink(0).Center().Radius(4).Background(m.theme.CardElevated).Children(func() {
			ui.Text(c, letter).FontSize(size * .64).Color(color).Bold()
		})
		return
	}
	if stem != "" {
		variant := "-dark"
		if m.light {
			variant = "-light"
		}
		bitmap := aiLogoBitmaps()[stem+variant+".png"]
		if bitmap.Width == 0 {
			bitmap = aiLogoBitmaps()[stem+".png"]
		}
		if bitmap.Width > 0 {
			ui.Image(c, bitmap).Size(size, size).Shrink(0)
			return
		}
	}
	ui.Icon(c, codeIcon).Size(size, size).Color(m.theme.MutedForeground)
}

var showSecretIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(2, 12), gfx.CubicTo(7, 4, 17, 4, 22, 12), gfx.CubicTo(17, 20, 7, 20, 2, 12),
	gfx.MoveTo(15, 12), gfx.CubicTo(15, 16, 9, 16, 9, 12), gfx.CubicTo(9, 8, 15, 8, 15, 12),
}}

var hideSecretIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(2, 12), gfx.CubicTo(7, 4, 17, 4, 22, 12), gfx.CubicTo(17, 20, 7, 20, 2, 12),
	gfx.MoveTo(15, 12), gfx.CubicTo(15, 16, 9, 16, 9, 12), gfx.CubicTo(9, 8, 15, 8, 15, 12),
	gfx.MoveTo(4, 4), gfx.LineTo(20, 20),
}}

var modelEditIcon = gfx.Icon{ViewBox: 24, Stroke: 1.7, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
	gfx.MoveTo(4, 16), gfx.LineTo(16.5, 3.5), gfx.QuadTo(18, 2, 20, 4), gfx.QuadTo(22, 6, 20.5, 7.5),
	gfx.LineTo(8, 20), gfx.LineTo(3, 21), gfx.ClosePath(), gfx.MoveTo(14.5, 5.5), gfx.LineTo(18.5, 9.5),
}}
