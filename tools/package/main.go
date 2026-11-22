package main

import (
	"fmt"
	"os"

	"github.com/vivaui/viva/tools/winpkg"
	"github.com/vivaui/viva/tools/winres"
)

func main() {
	if err := packageExecutable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func packageExecutable() error {
	icon, _, err := winpkg.LoadIcon("assets/icon.png")
	if err != nil {
		return err
	}
	path := "dist/EditorX.exe"
	if len(os.Args) == 2 {
		path = os.Args[1]
	}
	executable, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	executable, err = winres.Patch(executable, icon, winres.Info{Name: "EditorX", Description: "EditorX", Version: "0.32.1", File: "EditorX.exe"})
	if err != nil {
		return err
	}
	return os.WriteFile(path, executable, 0755)
}
