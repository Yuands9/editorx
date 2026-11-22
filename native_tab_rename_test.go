package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestNativeTabRenameEnter(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native rename key check is opt-in")
	}
	m := newTestEditorWithDocument()
	m.dataDirectory = t.TempDir()
	tab := m.active
	const name = "原生配置.ini"
	m.rename.begin(tab, nil)
	m.rename.field.SetText(name)

	var failure error
	completed := false
	stop := make(chan struct{})
	defer close(stop)
	err := app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		if err := pressNativeTestEnter(); err != nil {
			application.Quit()
			return err
		}
		go func() {
			ticker := time.NewTicker(40 * time.Millisecond)
			defer ticker.Stop()
			deadline := time.Now().Add(5 * time.Second)
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if completed {
							return
						}
						switch {
						case !m.rename.open && tab.title() == name:
							completed = true
						case time.Now().After(deadline):
							failure, completed = fmt.Errorf("the native rename field did not submit on Enter"), true
						}
						if completed {
							application.Quit()
						}
					})
				}
			}
		}()
		return nil
	})
	if err != nil || failure != nil || !completed {
		t.Fatalf("native rename on Enter failed: %v, %v", err, failure)
	}
}
