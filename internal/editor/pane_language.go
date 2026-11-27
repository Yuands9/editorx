package editor

// Keep the native editor commands and macro playback on the same language rules.
func (p *editorPane) configureCodeLanguage() {
	options, _ := p.text.Code()
	prefix := ""
	switch p.view.Snapshot.Language {
	case "Go", "Java", "JavaScript", "TypeScript", "C/C++", "C#", "Rust", "Kotlin":
		prefix = "//"
	case "Python", "Shell", "PowerShell", "YAML", "TOML", "Properties", "ENV":
		prefix = "#"
	case "SQL":
		prefix = "--"
	case "INI":
		prefix = ";"
	}
	if options.LineComment != prefix {
		options.LineComment = prefix
		p.text.SetCode(options)
	}
}
