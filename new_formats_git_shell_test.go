package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func previewFixturePDF() []byte {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	stream := "BT /F1 24 Tf 65 370 Td (Viva PDF preview) Tj ET"
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 420] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	}
	var offsets []int
	for i, item := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, item)
	}
	xref := pdf.Len()
	pdf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return pdf.Bytes()
}

func writeMindMapFixture(t *testing.T, file, entry, body string) {
	t.Helper()
	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	stream, err := archive.Create(entry)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPDFAndMindMapOpenReadOnlyAndRestore(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "预览.pdf")
	if err := os.WriteFile(pdf, previewFixturePDF(), 0600); err != nil {
		t.Fatal(err)
	}
	modern := filepath.Join(dir, "画布.xmind")
	legacy := filepath.Join(dir, "旧画布.xmind")
	writeMindMapFixture(t, modern, "content.json", `[{"title":"本周","rootTopic":{"title":"根<script>","children":{"attached":[{"title":"计划"}],"detached":[{"title":"待办"}]}}},{"title":"下周","rootTopic":{"title":"第二页"}}]`)
	writeMindMapFixture(t, legacy, "content.xml", `<xmap-content><sheet><title>旧画布</title><topic><title>根节点</title><children><topics type="attached"><topic><title>子节点</title></topic></topics></children></topic></sheet></xmap-content>`)
	store, err := openSessionStore(filepath.Join(dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	model := newEditorApp()
	store.attach(model)
	for _, path := range []string{pdf, modern, legacy} {
		model.load(path)
		tab := model.active
		if tab == nil || tab.preview == nil || !tab.readOnlyPreview() || aiTextTab(tab) || model.saveTab(tab, false) {
			t.Fatalf("preview did not open as a read-only tab for %s: %s", path, model.errorText)
		}
	}
	newMap := model.tabs[1].preview.mindMap
	oldMap := model.tabs[2].preview.mindMap
	if len(newMap) != 2 || len(newMap[0].Root.Children) != 2 || len(oldMap) != 1 || oldMap[0].Root.Children[0].Title != "子节点" {
		t.Fatal("a modern or legacy branch was not decoded")
	}
	var html bytes.Buffer
	if err := xmindTemplate.Execute(&html, struct{ Sheets []mindMapSheet }{newMap}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.String(), "根&lt;script&gt;") {
		t.Fatal("topic text was not safely rendered")
	}
	if err := store.save(); err != nil {
		t.Fatal(err)
	}
	restored, err := openSessionStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	tabs := restored.windows[0].tabs
	if len(tabs) != 3 || tabs[0].format() != "PDF" || tabs[2].format() != "XMind" {
		t.Fatal("preview tabs were not restored")
	}
}

func TestProjectGitStageCommitPushFetchAndPull(t *testing.T) {
	executable, err := exec.LookPath("git.exe")
	if err != nil {
		t.Skip("Git for Windows is required for the local integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 28*time.Second)
	defer cancel()
	root := t.TempDir()
	// 在项目目录下生成临时文件时，Git 也只发现本次测试创建的仓库。
	t.Setenv("GIT_CEILING_DIRECTORIES", root)
	work := filepath.Join(root, "工作区")
	remote := filepath.Join(root, "remote.git")
	peer := filepath.Join(root, "peer")
	for _, dir := range []string{work, remote} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	git := func(dir string, args ...string) {
		t.Helper()
		if _, err := runGitCommand(ctx, executable, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	action := func(operation gitAction, value string, files, open []string) gitResult {
		t.Helper()
		result := runProjectGit(ctx, work, operation, value, files, open)
		if result.fail != "" {
			t.Fatalf("%s: %s", operation, result.fail)
		}
		return result
	}
	git(remote, "init", "--bare", "--initial-branch=main")
	action(gitInit, "", nil, nil)
	git(work, "symbolic-ref", "HEAD", "refs/heads/main")
	git(work, "config", "user.name", "Viva Fixture")
	git(work, "config", "user.email", "viva-test@example.invalid")
	const name = "中文[原样].txt"
	file := filepath.Join(work, name)
	if err := os.WriteFile(file, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	status := action(gitRefresh, "", nil, nil).status
	if len(status.changes) != 1 || status.changes[0].path != name || status.changes[0].staged() {
		t.Fatalf("unexpected Git status for the fixture: %+v", status)
	}
	if staged := action(gitStage, "", []string{name}, nil).status; !staged.hasStaged() {
		t.Fatal("Git file was not staged")
	}
	if unstaged := action(gitUnstage, "", []string{name}, nil).status; unstaged.hasStaged() {
		t.Fatal("Git file was not unstaged on the initial branch")
	}
	action(gitStageAll, "", nil, nil)
	action(gitCommit, "first fixture commit", nil, nil)
	action(gitAddRemote, remote, nil, nil)
	action(gitPush, "origin", nil, nil)
	git(root, "clone", remote, peer)
	git(peer, "config", "user.name", "Viva Peer")
	git(peer, "config", "user.email", "viva-test@example.invalid")
	if err := os.WriteFile(filepath.Join(peer, name), []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(peer, "add", "--", name)
	git(peer, "commit", "-m", "second fixture commit")
	git(peer, "push")
	action(gitFetch, "", nil, nil)
	pulled := action(gitPull, "", nil, []string{file})
	if pulled.updated[file].saved != "second\n" || len(pulled.status.changes) != 0 {
		t.Fatalf("pull did not reload the changed fixture: %+v", pulled)
	}
}

func TestPowerShellPTYRunsGitInProject(t *testing.T) {
	if _, err := exec.LookPath("git.exe"); err != nil {
		t.Skip("Git for Windows is required for the local integration test")
	}
	process, err := startTerminalProcess(t.TempDir(), terminalSize{cols: 90, rows: 25})
	if err != nil {
		t.Fatal(err)
	}
	defer process.close()
	output := make(chan string, 256)
	exited := make(chan struct{})
	process.run(func(data []byte) { output <- string(data) }, func() { close(exited) })
	if !process.enqueue(terminalInput{text: "git --version\r"}) ||
		!process.enqueue(terminalInput{text: "Write-Output ('VIVA_' + 'SHELL_OK')\r"}) {
		t.Fatal("PowerShell input could not be queued")
	}
	deadline := time.NewTimer(16 * time.Second)
	defer deadline.Stop()
	var value strings.Builder
	for !strings.Contains(value.String(), "git version") || !strings.Contains(value.String(), "VIVA_SHELL_OK") {
		select {
		case chunk := <-output:
			value.WriteString(chunk)
		case <-exited:
			t.Fatalf("PowerShell exited early: %q", value.String())
		case <-deadline.C:
			t.Fatalf("PowerShell command did not finish: %q", value.String())
		}
	}
	process.enqueue(terminalInput{text: "exit\r"})
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("PowerShell did not exit cleanly")
	}
}
