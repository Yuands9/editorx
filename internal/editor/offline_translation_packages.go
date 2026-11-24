package editor

// Official, versioned Windows x64 archives. The installer never invokes pip or
// downloads a package index or a full Argos/PyTorch environment. These pinned
// inference APIs do not use NumPy, PyYAML, converters or training utilities.
type offlineArchive struct {
	URL, SHA256 string
	Bytes       int64
}

type offlineComponent struct {
	ID, Label, Prefix string
	Archives          []offlineArchive
	Required          []string
}

var offlineRuntime = offlineComponent{
	ID: "runtime-1", Label: "运行时",
	Required: []string{"python.exe", "python313.dll", "python313.zip", "msvcp140.dll", "ctranslate2/ctranslate2.dll", "sentencepiece/__init__.py"},
	Archives: []offlineArchive{
		{"https://www.python.org/ftp/python/3.13.17/python-3.13.17-embed-amd64.zip", "9e35319bffedab8b36ad44d8f377cb4f1733bcec257a0b6d04d94f01486ddb38", 11441267},
		{"https://files.pythonhosted.org/packages/4f/5c/ed3c4835ea69da2813a50d3c1b4b1b3c5145703eb615f93f2f4981850e2f/ctranslate2-4.9.1-cp313-cp313-win_amd64.whl", "cccffe88910e2ca5b8383d5ef323734dc03e200f5d3cf9949ece406b72423312", 23105788},
		{"https://files.pythonhosted.org/packages/a3/39/41b3c56beb8ced46bfb346a638ca588741fcd10b6e1143d9143787e1c66d/sentencepiece-0.2.3-cp313-cp313-win_amd64.whl", "84e9def430ca8cc848b84adb304ffb77bff151cfc7dc66eb881f0385258f536b", 1655543},
	},
}

var offlineModels = [...]offlineComponent{
	{ID: "en-zh-1.9", Label: "英语 → 简体中文", Prefix: "translate-en_zh-1_9/",
		Required: []string{"model/model.bin", "model/config.json", "model/shared_vocabulary.json", "sentencepiece.model", "metadata.json", "README.md"},
		Archives: []offlineArchive{{"https://argos-net.com/v1/translate-en_zh-1_9.argosmodel", "433e7c4f034d87fbe2353161e05f18646d7999452f801a4e1f0378522b9850ab", 70743021}}},
	{ID: "zh-en-1.9", Label: "简体中文 → 英语", Prefix: "translate-zh_en-1_9/",
		Required: []string{"model/model.bin", "model/config.json", "model/shared_vocabulary.json", "sentencepiece.model", "metadata.json", "README.md"},
		Archives: []offlineArchive{{"https://argos-net.com/v1/translate-zh_en-1_9.argosmodel", "62e7af5a3a48b530e47b7b3e5c78c2de79073ecd815750d2bf3ab35b4a67da2d", 74481402}}},
}

func (component offlineComponent) downloadBytes() int64 {
	var total int64
	for _, archive := range component.Archives {
		total += archive.Bytes
	}
	return total
}

func offlineLanguageModel(source, target string) *offlineComponent {
	if source == "en" && target == "zh-cn" {
		return &offlineModels[0]
	}
	if source == "zh-cn" && target == "en" {
		return &offlineModels[1]
	}
	return nil
}
