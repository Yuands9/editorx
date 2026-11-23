//go:build !editorx_public

package editor

import "embed"

const (
	officeAssetDirectory = "assets/office"
	hasWordEditor        = true
)

//go:embed assets/office
var officeAssets embed.FS
