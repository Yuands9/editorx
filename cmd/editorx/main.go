package main

import (
	"os"

	"github.com/coderyds/editorx/internal/editor"
)

func main() {
	editor.Run(os.Args[1:])
}
