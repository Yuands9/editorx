//go:build editorx_public

package editor

import "embed"

const (
	officeAssetDirectory = "assets/office-public"
	hasWordEditor        = false
)

//go:embed assets/office-public
var officeAssets embed.FS
