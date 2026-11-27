package editor

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

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func previewFixturePDF() []byte {
	stream := "BT /F1 24 Tf 65 370 Td (Viva PDF preview) Tj ET"
	return makeFixturePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 500 420] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
	)
}

func makeFixturePDF(objects ...string) []byte {
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	var offsets []int
	for i, item := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, item)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	m := newEditorApp()
	m.load(file)
	m.openProjectGit(work)
	session := openTestSession(t, m)
	waitForGit := func() {
		t.Helper()
		if err := session.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 12*time.Second); err != nil {
			t.Fatal(err)
		}
		if m.tools.git.issue != "" {
			t.Fatal(m.tools.git.issue)
		}
	}
	click := func(label string) {
		t.Helper()
		if err := session.ClickLabel(label); err != nil {
			t.Fatal(err)
		}
	}
	waitForGit()
	click("checkbox:选择提交 · " + name)
	if m.tools.git.request != nil || len(m.tools.git.selectedCommitPaths()) != 1 {
		t.Fatal("choosing the staged file must select it without running Git")
	}
	click("提交说明")
	entry, err := session.Locate("提交说明")
	if err != nil {
		t.Fatal(err)
	}
	for _, primitive := range session.Snapshot().Primitives {
		if primitive.Bounds == entry.Bounds && primitive.Color == m.theme.Primary {
			t.Fatal("the focused commit description shows a bright border")
		}
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := session.Screenshot(filepath.Join(directory, "git-no-input-border.png")); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.Type("first fixture commit"); err != nil {
		t.Fatal(err)
	}
	click("button:提交")
	waitForGit()
	if m.tools.git.snapshot.hasStaged() || m.tools.git.commit.Text() != "" {
		t.Fatal("the commit button did not commit and clear the description")
	}
	click("远程仓库地址")
	if err := session.Type(remote); err != nil {
		t.Fatal(err)
	}
	click("button:添加远程")
	waitForGit()
	click("button:推送")
	waitForGit()
	if m.tools.git.snapshot.tracking == "" {
		t.Fatal("the push button did not publish the branch")
	}
	git(root, "clone", remote, peer)
	git(peer, "config", "user.name", "Viva Peer")
	git(peer, "config", "user.email", "viva-test@example.invalid")
	if err := os.WriteFile(filepath.Join(peer, name), []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(peer, "add", "--", name)
	git(peer, "commit", "-m", "second fixture commit")
	git(peer, "push")
	click("button:获取")
	waitForGit()
	click("button:拉取")
	waitForGit()
	mainFile := m.active
	if mainFile.source.text.CommittedString() != "second\n" || len(m.tools.git.snapshot.changes) != 0 {
		t.Fatal("the pull button did not reload the changed file in the editor")
	}

	click("button:新建分支")
	click("分支名称")
	if err := session.Type("feature/local"); err != nil {
		t.Fatal(err)
	}
	if err := session.Driver().Key(gfx.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	waitForGit()
	if m.tools.git.snapshot.branch != "feature/local" || m.tools.git.branches.creating {
		t.Fatal("the new branch was not created and selected")
	}
	click("button:切换分支")
	waitForGit()
	if _, err := session.Locate("button:main"); err != nil {
		t.Fatal(err)
	}
	if directory := os.Getenv("VIVA_PREVIEW_DIR"); directory != "" {
		if err := session.Screenshot(filepath.Join(directory, "git-branch-picker.png")); err != nil {
			t.Fatal(err)
		}
	}
	click("button:main")
	waitForGit()

	click("button:切换分支")
	waitForGit()
	click("button:feature/local")
	waitForGit()
	branchOnly := filepath.Join(work, "feature-only.txt")
	if err := os.WriteFile(branchOnly, []byte("only on this branch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("feature branch\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(work, "add", "-A")
	git(work, "commit", "-m", "feature change")
	m.load(branchOnly)
	branchTab := m.active
	click("button:切换分支")
	waitForGit()
	click("button:main")
	waitForGit()
	if branchTab.doc.path != "" || !branchTab.modified() || branchTab.source.text.String() != "only on this branch\n" {
		t.Fatal("a file missing on the new branch was not kept as a draft")
	}
	if mainFile.source.text.String() != "second\n" {
		t.Fatal("switching to main changed its file")
	}

	click("button:切换分支")
	waitForGit()
	click("button:feature/local")
	waitForGit()
	if mainFile.source.text.String() != "feature branch\n" {
		t.Fatal("switching branches did not reload an unmodified tab")
	}
	mainFile.source.replace("uncommitted editor draft\n")
	click("button:切换分支")
	waitForGit()
	click("button:main")
	waitForGit()
	if !mainFile.modified() || mainFile.source.text.String() != "uncommitted editor draft\n" {
		t.Fatal("switching branches overwrote an unsaved draft")
	}
	if err := os.WriteFile(file, []byte("unstaged disk modification\n"), 0600); err != nil {
		t.Fatal(err)
	}
	click("button:切换分支")
	waitForGit()
	click("button:feature/local")
	if err := session.WaitFor(func(app.Snapshot) bool { return m.tools.git.request == nil }, 12*time.Second); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(file)
	if err != nil || m.tools.git.issue == "" || m.tools.git.snapshot.branch != "main" || string(content) != "unstaged disk modification\n" {
		t.Fatalf("switching branches did not preserve a conflicting working copy: %v", err)
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
