package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestNativeOpenTiming(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native timing is opt-in")
	}
	directory := t.TempDir()
	webData := os.Getenv("VIVA_NATIVE_OPEN_PROFILE")
	if webData == "" {
		webData = filepath.Join(directory, "WebView2")
	}
	t.Setenv("WEBVIEW2_USER_DATA_FOLDER", webData)
	git, err := exec.LookPath("git.exe")
	if err != nil {
		t.Skip("Git for Windows is required for startup timing")
	}
	if _, err := runGitCommand(context.Background(), git, directory, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	sheet := filepath.Join(directory, "中文.xlsx")
	word := filepath.Join(directory, "中文.docx")
	pdf := filepath.Join(directory, "页面 & 说明 + 预览.pdf")
	for path, data := range map[string][]byte{sheet: sampleWorkbook(t), word: sampleDocx(t), pdf: previewFixturePDF()} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newEditorApp()
	m.dataDirectory = directory
	samples := []struct {
		label string
		open  func()
		ready func() (bool, string)
	}{
		{"Excel", func() { m.load(sheet) }, func() (bool, string) {
			return m.active.office.ready, m.active.office.errorText
		}},
		{"Word", func() { m.load(word) }, func() (bool, string) {
			return m.active.office.ready, m.active.office.errorText
		}},
		{"PDF", func() { m.load(pdf) }, func() (bool, string) {
			return m.active.preview.ready, m.active.preview.issue
		}},
		{"PowerShell", func() { m.openLocalShell(directory) }, func() (bool, string) {
			return m.tools.terminal.viewReady && m.tools.terminal.process != nil, m.tools.terminal.issue
		}},
		{"Git", func() { m.openProjectGit(directory) }, func() (bool, string) {
			return m.tools.git.request == nil && m.tools.git.snapshot.root != "", m.tools.git.issue
		}},
	}
	var failure error
	var started time.Time
	checkingPDF := false
	sample := 0
	completed := false
	stop := make(chan struct{})
	defer close(stop)
	err = app.RunApplication(func(application *app.Application) error {
		if err := openEditorWindow(application, m); err != nil {
			return err
		}
		finish := func(err error) {
			failure, completed = err, true
			m.releaseWindowState()
			application.Quit()
		}
		go func() {
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					application.Post(func() {
						if completed || !m.webReady {
							return
						}
						current := samples[sample]
						if started.IsZero() {
							started = time.Now()
							current.open()
							t.Logf("%s UI command returned in %s", current.label, time.Since(started).Round(time.Millisecond))
							m.invalidator.Invalidate()
							return
						}
						if m.showError {
							finish(fmt.Errorf("%s: %s", current.label, m.errorText))
							return
						}
						ready, problem := current.ready()
						if problem != "" || time.Since(started) > 20*time.Second {
							finish(fmt.Errorf("%s did not open: %s", current.label, problem))
							return
						}
						if !ready {
							return
						}
						if current.label == "PDF" {
							if !checkingPDF {
								t.Logf("PDF first page ready in %s", time.Since(started).Round(time.Millisecond))
								checkingPDF = true
								preview := m.active.preview
								preview.ready = false
								m.setLightMode(true)
								preview.view.Eval(`if (getComputedStyle(document.querySelector('#viewer canvas')).filter !== 'none') {window.viva.postMessage(JSON.stringify({kind:'error', text:'PDF light paper did not follow the theme'}))}`)
								m.setLightMode(false)
								preview.view.Eval(checkPDFPreviewJS)
								return
							}
							t.Log("PDF title, zoom, paper theme, search, and original file range passed")
						} else {
							t.Logf("%s ready in %s", current.label, time.Since(started).Round(time.Millisecond))
						}
						sample++
						started = time.Time{}
						if sample == len(samples) {
							finish(nil)
						}
					})
				}
			}
		}()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if failure != nil {
		t.Fatal(failure)
	}
	if !completed {
		t.Fatal("native startup timing ended early")
	}
}

const checkPDFPreviewJS = `(async () => {
	try {
		if (getComputedStyle(document.querySelector("#viewer canvas")).filter === "none") {
			throw new Error("PDF dark paper did not follow the theme");
		}
		if (document.querySelector("#name").textContent !== "页面 & 说明 + 预览.pdf") {
			throw new Error("PDF title was not displayed");
		}
		const zoom = () => parseFloat(document.querySelector("#fit").textContent);
		const initial = zoom();
		document.querySelector("#larger").click();
		const enlarged = zoom();
		document.querySelector("#smaller").click();
		if (!(enlarged > initial && zoom() < enlarged)) {
			throw new Error("PDF zoom buttons did not change the scale");
		}
		const response = await fetch("document.pdf", {headers: {Range: "bytes=0-4"}});
		if (response.status !== 206 || await response.text() !== "%PDF-") {
			throw new Error("PDF range request did not return the original bytes");
		}
		document.querySelector("#find-toggle").click();
		const field = document.querySelector("#find-query");
		field.value = "Viva PDF preview";
		field.dispatchEvent(new Event("input"));
		const deadline = Date.now() + 3000;
		while (document.querySelector("#find-count").textContent !== "1 / 1") {
			if (Date.now() >= deadline) throw new Error("PDF text search did not return the expected match");
			await new Promise(resolve => setTimeout(resolve, 40));
		}
		window.viva.postMessage(JSON.stringify({kind: "ready"}));
	} catch (error) {
		window.viva.postMessage(JSON.stringify({kind: "error", text: error.message}));
	}
})()`
