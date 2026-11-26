package version

import "runtime/debug"

const Number = "0.38.0"

// BuildDate is set by dev.ps1 when packaging the executable.
var BuildDate string

func Build() string {
	var revision string
	var modified bool
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value[:min(7, len(setting.Value))]
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if revision != "" && modified {
		revision += "（含本地改动）"
	}
	if revision != "" && BuildDate != "" {
		return revision + " · " + BuildDate
	}
	if revision != "" {
		return revision
	}
	if BuildDate != "" {
		return BuildDate
	}
	return "未记录"
}
