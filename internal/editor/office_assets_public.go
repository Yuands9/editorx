//go:build editorx_public

package editor

import "embed"

const (
	officeAssetDirectory = "assets/office-public/packed"
	hasWordEditor        = false
)

//go:embed assets/office-public/packed
var officeAssets embed.FS
