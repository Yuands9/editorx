//go:build !editorx_public

package editor

import "embed"

const (
	officeAssetDirectory = "assets/office/packed"
	hasWordEditor        = true
)

//go:embed assets/office/packed
var officeAssets embed.FS
