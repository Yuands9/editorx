package editor

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

const testVaultMaster = "本地演示 passphrase 2026"

func TestPasswordVaultAuthenticatedStorage(t *testing.T) {
	path := passwordVaultPath(t.TempDir())
	vault, err := createPasswordVault(testVaultMaster)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.clear()
	data := vault.data.clone()
	data.DefaultGroup = 0
	data.Next = 2
	data.Groups = []passwordGroup{{ID: 1, Name: "测试服务器"}, {ID: 2, Name: "正式服务器", Production: true}}
	data.Next++
	data.Entries = []passwordEntry{{ID: data.Next, Group: 1, Kind: vaultLogin, Name: "演示运维账号", Site: "https://login.example.com/", Account: "sample-user", Secret: " example-secret+{} "}}
	if err := vault.save(path, data); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, cleartext := range []string{testVaultMaster, data.Entries[0].Secret, data.Entries[0].Account, "测试服务器"} {
		if bytes.Contains(encoded, []byte(cleartext)) {
			t.Fatal("plaintext found in encrypted file")
		}
	}
	if _, err := unlockPasswordVault(encoded, "wrong example password"); !errors.Is(err, errVaultUnlock) {
		t.Fatal("wrong password accepted", err)
	}
	tampered := bytes.Clone(encoded)
	tampered[len(tampered)-1] ^= 1
	if _, err := unlockPasswordVault(tampered, testVaultMaster); !errors.Is(err, errVaultUnlock) {
		t.Fatal("tampered file accepted", err)
	}
	stale, err := unlockPasswordVault(encoded, testVaultMaster)
	if err != nil {
		t.Fatal(err)
	}
	defer stale.clear()
	if stale.data.Entries[0].Secret != data.Entries[0].Secret {
		t.Fatal("password whitespace or punctuation changed")
	}
	if err := vault.save(path, data); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(path)
	if err != nil || bytes.Equal(encoded, current) {
		t.Fatal("successive writes reused the same encrypted encoding", err)
	}
	if err := stale.save(path, stale.data); !errors.Is(err, errVaultChanged) {
		t.Fatal("stale edit did not detect a version change", err)
	}
	restoredPath := passwordVaultPath(t.TempDir())
	restored, err := restorePasswordVault(restoredPath, path, testVaultMaster)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.clear()
	if restored.data.Entries[0].Account != "sample-user" || !restored.data.Groups[1].Production ||
		restored.data.DefaultGroup != 0 || restored.data.Groups[1].Parent != 0 {
		t.Fatal("backup did not restore legacy entries and flat groups")
	}
	if _, err := restorePasswordVault(path, restoredPath, testVaultMaster); !errors.Is(err, errVaultChanged) {
		t.Fatal("restoring on top of an existing vault was allowed", err)
	}
}

func TestPasswordVaultGroupValidation(t *testing.T) {
	data := newPasswordVaultData()
	data.Next = 5
	data.Groups = append(data.Groups, passwordGroup{ID: 2, Name: "项目"},
		passwordGroup{ID: 3, Parent: 1, Name: "部署"}, passwordGroup{ID: 4, Parent: 2, Name: "部署"},
		passwordGroup{ID: 5, Parent: 3, Name: "测试"})
	if err := validatePasswordVault(data); err != nil {
		t.Fatal(err)
	}
	if path := data.groupPath(5); path != "默认分组 / 部署 / 测试" {
		t.Fatalf("unexpected nested group path %q", path)
	}
	for _, parent := range []int{1, 4, 6} {
		// Reparenting the second 部署 creates a duplicate, a self-cycle or a missing parent.
		altered := data.clone()
		altered.Groups[3].Parent = parent
		if err := validatePasswordVault(altered); err == nil {
			t.Fatalf("invalid parent %d was accepted", parent)
		}
	}
}

func TestPasswordVaultImportFormats(t *testing.T) {
	input := "1、\njob.example.com/admin/login\n账号：sample-admin 密码：sample?pass\n\n2、\n| 演示机 | [登录](https://192.0.2.45/) | `sample-reader` | `sample\\|key` |\n| --- | --- | --- | --- |\n"
	rows := detectPasswordEntries(input)
	if len(rows) != 2 || rows[0].entry.Site != "https://job.example.com/admin/login" || rows[0].entry.Secret != "sample?pass" ||
		rows[1].entry.Site != "https://192.0.2.45/" || rows[1].entry.Account != "sample-reader" || rows[1].entry.Secret != "sample|key" {
		t.Fatal("mixed text and Markdown rows were not correctly mapped")
	}
	rows = detectPasswordEntries("| 口令 | 系统 | 用户名 | 网站 |\n| --- | --- | --- | --- |\n| | 待补齐 | sample-admin | https://test.example.com |")
	if len(rows) != 1 || rows[0].entry.Name != "待补齐" || validatePasswordEntry(rows[0].entry) == nil {
		t.Fatal("incomplete table data must remain a draft")
	}
	if len(detectPasswordEntries("普通工作日志\njavascript:alert(1)")) != 0 {
		t.Fatal("unrelated text was treated as credentials")
	}
	if normalizePasswordSite("https://sample:secret@example.com/") != "" || normalizePasswordSite("javascript:alert(1)") != "" {
		t.Fatal("unsafe scheme or URL user credentials recognized")
	}
}

func paintedTextInBounds(texts []gfx.Text, value string, bounds gfx.Rect) bool {
	for _, item := range texts {
		x, y := item.Bounds.X, item.Bounds.Y+item.Bounds.Height/2
		if item.Value == value && x >= bounds.X && x < bounds.X+bounds.Width && y >= bounds.Y && y < bounds.Y+bounds.Height {
			return true
		}
	}
	return false
}

func TestPasswordVaultThroughUI(t *testing.T) {
	const master = "样例Ab1234"
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	tab := m.addTab(document{saved: "演示网页\nhttps://jump.example.com/\n账号：sample-editor 密码：sample-paste"}, "demo.md")
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) { t.Helper(); check(s.ClickLabel(label)); check(s.Frame()) }
	check(s.Resize(1080, 720))
	if _, err := os.Stat(passwordVaultPath(m.dataDirectory)); !errors.Is(err, os.ErrNotExist) || m.passwordVault != nil {
		t.Fatal("vault was activated without an explicit action")
	}
	click("主菜单")
	click("menuitem:密码本")
	v := m.passwordVault
	if v == nil || !m.vaultActive() || !s.HasText("创建本机密码本") || !s.HasText("请选择至少 8 个字符的主密码。忘记主密码将无法解锁。") {
		t.Fatal("vault menu did not show the eight-character creation policy")
	}
	expectPasswordEye := func(label string, masked bool, visibleValue string) {
		t.Helper()
		field, err := s.Locate(label)
		check(err)
		caption := "隐藏" + label
		if masked {
			caption = "显示" + label
		}
		eye, err := s.Locate("button:" + caption)
		check(err)
		if field.Secure != masked || field.Value != visibleValue ||
			eye.Bounds.X < field.Bounds.X || eye.Bounds.X+eye.Bounds.Width > field.Bounds.X+field.Bounds.Width {
			t.Fatalf("%s has incorrect visibility or eye position", label)
		}
		if !eye.SkipTab {
			t.Fatalf("%s visibility button received a Tab stop", label)
		}
		if !masked && !paintedTextInBounds(s.Snapshot().Texts, visibleValue, field.Bounds) {
			t.Fatalf("%s plaintext is not painted inside the input", label)
		}
	}
	textBounds := func(label string) gfx.Rect {
		t.Helper()
		snapshot := s.Snapshot()
		for index, text := range snapshot.Texts {
			if text.Value == label {
				bounds := text.Bounds
				bounds.Width = snapshot.TextWidths[index]
				return bounds
			}
		}
		t.Fatalf("text %q was not painted", label)
		return gfx.Rect{}
	}
	click("主密码")
	check(s.Driver().Key(gfx.KeyTab, 0))
	if s.Driver().Snapshot().Focused != v.confirmation.ID {
		t.Fatal("Tab from the master password did not focus confirmation")
	}
	check(s.Driver().Key(gfx.KeyTab, gfx.ModifierShift))
	if s.Driver().Snapshot().Focused != v.password.ID {
		t.Fatal("Shift+Tab from confirmation did not focus the master password")
	}
	v.password.SetText(master)
	v.confirmation.SetText(master)
	check(s.Frame())
	expectPasswordEye("主密码", true, "")
	expectPasswordEye("确认主密码", true, "")
	click("button:显示主密码")
	expectPasswordEye("主密码", false, master)
	expectPasswordEye("确认主密码", true, "")
	click("主密码")
	v.password.Select(0, 2)
	check(s.Frame())
	if s.Driver().Snapshot().Focused != v.password.ID {
		t.Fatal("password input did not receive focus")
	}
	handled, err := s.Driver().ShortcutChord(gfx.ModifierControl, 'c')
	check(err)
	if !handled || v.password.Text() != master || s.Driver().Clipboard() != "样例" {
		t.Fatal("Ctrl+C did not copy only the selected characters from the revealed password")
	}
	v.password.Select(0, v.password.GetEditor().RuneCount())
	check(s.Frame())
	handled, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'c')
	check(err)
	if !handled || s.Driver().Clipboard() != master {
		t.Fatal("could not copy the full revealed password")
	}
	v.password.GetEditor().MoveTo(v.password.GetEditor().RuneCount(), false)
	check(s.Frame())
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "password-vault-gate.png")))
	}
	click("button:隐藏主密码")
	expectPasswordEye("主密码", true, "")
	v.confirmation.SetText("")
	click("确认主密码")
	handled, err = s.Driver().ShortcutChord(gfx.ModifierControl, 'v')
	check(err)
	if !handled || v.confirmation.Text() != master {
		t.Fatal("could not paste the copied password into the confirmation")
	}
	click("button:显示确认主密码")
	expectPasswordEye("确认主密码", false, master)
	expectPasswordEye("主密码", true, "")
	click("button:隐藏确认主密码")
	expectPasswordEye("确认主密码", true, "")

	check(s.Driver().QueueOpenFiles([]string{filepath.Join(m.dataDirectory, "sample.edxvault")}, nil))
	click("button:从加密备份恢复…")
	expectPasswordEye("备份密码", true, "")
	v.password.SetText(master)
	check(s.Frame())
	click("button:显示备份密码")
	expectPasswordEye("备份密码", false, master)
	click("button:返回创建")
	expectPasswordEye("主密码", true, "")
	expectPasswordEye("确认主密码", true, "")
	const tooShort = "样例Ab123"
	v.password.SetText(tooShort)
	v.confirmation.SetText(tooShort)
	click("button:创建密码本")
	if v.store != nil || !strings.Contains(v.problem, "8 个字符") {
		t.Fatal("the creation form did not reject a seven-character master password")
	}
	v.password.SetText(master)
	v.confirmation.SetText(master)
	click("button:创建密码本")
	if v.store == nil || !s.HasText("本机加密库") {
		t.Fatal("vault did not unlock after creation", v.problem)
	}
	const defaultName = "默认分组"
	defaultGroup := v.store.data.DefaultGroup
	if len(v.store.data.Groups) != 1 || defaultGroup <= 0 || v.store.data.group(defaultGroup).Name != defaultName {
		t.Fatal("new vault did not create exactly one default group")
	}
	if _, err := s.Locate("未分组"); err == nil {
		t.Fatal("new vault exposed the legacy ungrouped view")
	}
	click("button:新建条目")
	if v.dialog.group != defaultGroup {
		t.Fatal("new entry from all items did not select the default group")
	}
	click("button:取消")
	newChild := func(parent, name string) int {
		t.Helper()
		check(s.RightClickLabel(parent))
		click("menuitem:新建子分组…")
		if _, err := s.Locate("switch:正式环境标识"); err == nil {
			t.Fatal("new group dialog still offered the production flag")
		}
		v.dialog.name.SetText(name)
		click("button:保存分组")
		return v.group
	}
	testGroup := newChild(defaultName, "测试服务器")
	productionGroup := newChild(defaultName, "正式服务器")
	if v.store.data.group(testGroup).Parent != defaultGroup || v.store.data.group(productionGroup).Parent != defaultGroup {
		t.Fatal("context menu did not use the group that was right-clicked")
	}
	click(defaultName)
	check(s.Keys("left"))
	if _, err := s.Locate("测试服务器"); err == nil {
		t.Fatal("collapsed group still showed its children")
	}
	check(s.Keys("right"))
	click("测试服务器")
	if textBounds("全部条目").X != textBounds(defaultName).X || textBounds("测试服务器").X != textBounds("正式服务器").X ||
		textBounds("测试服务器").X <= textBounds(defaultName).X {
		t.Fatal("group names did not align by depth")
	}
	if v.group != testGroup {
		t.Fatal("expanded child was not selected")
	}
	click("button:从文本识别")
	d := v.dialog
	d.source.SetText("| 运维 | https://deploy.example.com/test | sample-ops | sample-one |\n| --- | --- | --- | --- |\n| 只读 | [登录](https://deploy.example.com/test) | sample-readonly | sample-two |")
	check(s.Frame())
	if len(d.importRows) != 2 || d.group != testGroup || len(v.store.data.Entries) != 0 {
		t.Fatal("import did not stage two rows in the active group")
	}
	if textBounds("运维").X != textBounds("只读").X {
		t.Fatal("import candidates are not left aligned")
	}
	expectPasswordEye("密码", true, "")
	click("button:显示密码")
	expectPasswordEye("密码", false, "sample-one")
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "password-vault-import-revealed.png")))
	}
	click("button:隐藏密码")
	expectPasswordEye("密码", true, "")
	click("button:显示密码")
	click("button:只读")
	expectPasswordEye("密码", true, "")
	click("button:运维")
	expectPasswordEye("密码", true, "")
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "password-vault-import.png")))
	}
	click("button:保存 2 条")
	siteLabel := "deploy.example.com · 2 个账号"
	if textBounds("运维 · sample-ops").X != textBounds("只读 · sample-readonly").X ||
		textBounds("运维 · sample-ops").X <= textBounds(siteLabel).X {
		t.Fatal("account labels did not align within their site")
	}
	click("button:" + siteLabel)
	if _, err := s.Locate("button:只读 · sample-readonly"); err == nil {
		t.Fatal("the left-aligned site row did not collapse its accounts")
	}
	click("button:" + siteLabel)
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "password-vault-groups.png")))
	}
	if len(v.store.data.Entries) != 2 || len(passwordSiteBuckets(v.store.data.Entries)) != 1 {
		t.Fatal("same-site accounts were not kept as two credentials")
	}
	click("button:添加同站点账号")
	if v.dialog.site.Text() != "https://deploy.example.com/test" || v.dialog.group != testGroup || v.dialog.account.Text() != "" || v.dialog.secret.Text() != "" {
		t.Fatal("same-site action did not prefill only the website and group")
	}
	click("button:取消")
	check(s.RightClickLabel("正式服务器"))
	click("menuitem:新建条目…")
	if v.dialog.group != productionGroup {
		t.Fatal("new entry did not use the right-clicked group")
	}
	v.dialog.name.SetText("正式账号")
	v.dialog.site.SetText("https://deploy.example.com/test")
	v.dialog.account.SetText("sample-production")
	v.dialog.secret.SetText("sample-live-secret")
	v.dialog.tags.SetText("运维，服务器")
	v.dialog.note.SetText("登录时使用备用账号。")
	check(s.Frame())
	var previousFieldY float32
	for _, label := range []string{"网站", "用户名", "密码"} {
		field, err := s.Locate("textfield:" + label)
		check(err)
		if field.Bounds.Y <= previousFieldY {
			t.Fatal("credential fields in the editor are out of order")
		}
		previousFieldY = field.Bounds.Y
	}
	expectPasswordEye("密码", true, "")
	click("button:显示密码")
	expectPasswordEye("密码", false, "sample-live-secret")
	click("button:隐藏密码")
	expectPasswordEye("密码", true, "")
	click("button:保存条目")
	if v.group != productionGroup || len(passwordSiteBuckets(v.store.data.Entries)) != 2 {
		t.Fatal("same site crossed the production boundary")
	}
	for _, width := range []int{1440, 900} {
		check(s.Resize(width, 720))
		eye, err := s.Locate("button:显示密码")
		check(err)
		if !eye.SkipTab {
			t.Fatal("the credential details visibility button received a Tab stop")
		}
		site, user, password := textBounds("网站"), textBounds("用户名"), textBounds("密码")
		if site.X != user.X || user.X != password.X || site.Y >= user.Y || user.Y >= password.Y {
			t.Fatal("credential detail labels are out of order or alignment")
		}
		account := textBounds("sample-production")
		copy, err := s.Locate("button:复制用户名")
		check(err)
		if gap := copy.Bounds.X - (account.X + account.Width); gap < 0 || gap > 14 {
			t.Fatalf("copy button is too far from the account value: %.1f", gap)
		}
		if tags, note := textBounds("标签"), textBounds("备注"); tags.Y <= password.Y || note.Y <= tags.Y ||
			!s.HasText("服务器") || !s.HasText("登录时使用备用账号。") {
			t.Fatal("credential metadata is missing its sections or contents")
		}
		if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
			name := "password-vault-detail.png"
			if width == 900 {
				name = "password-vault-detail-narrow.png"
			}
			check(s.Screenshot(filepath.Join(output, name)))
		}
	}
	check(s.Resize(1080, 720))
	click("button:新建分组")
	v.dialog.name.SetText("自定义演示分组")
	click("button:保存分组")
	if v.store.data.group(v.group).Name != "自定义演示分组" {
		t.Fatal("new group was not saved")
	}
	click("正式服务器")
	click("button:复制密码")
	if s.Driver().Clipboard() != "sample-live-secret" {
		t.Fatal("copy did not use the selected credential")
	}
	if s.HasText("sample-live-secret") {
		t.Fatal("masked password exposed in the viewport")
	}
	if snapshot, err := json.Marshal(m.snapshotSession()); err != nil || bytes.Contains(snapshot, []byte("sample-live-secret")) || len(m.tabs) != 1 || m.active != tab {
		t.Fatal("vault entered the document session")
	}
	click("button:锁定密码本")
	if v.store != nil || s.Driver().Clipboard() != "" {
		t.Fatal("locking did not clear the unlocked state and clipboard")
	}
	v.password.SetText(master)
	check(s.Frame())
	expectPasswordEye("主密码", true, "")
	click("button:显示主密码")
	expectPasswordEye("主密码", false, master)
	check(s.Keys("escape"))
	expectPasswordEye("主密码", true, "")
	click("button:解锁")
	if v.store == nil || len(v.store.data.Entries) != 3 || v.store.data.DefaultGroup != defaultGroup ||
		v.store.data.group(productionGroup).Parent != defaultGroup || v.store.data.group(productionGroup).Production {
		t.Fatal("unlock did not restore credentials and the group tree")
	}
	v.lastUse = time.Now().Add(-6 * time.Minute)
	check(s.Frame())
	if v.store != nil {
		t.Fatal("idle expiration did not lock the vault")
	}
	m.activate(tab)
	tab.markdownMode = "编辑"
	tab.source.text.SetSelection(text.Range{Start: 0, End: tab.source.text.RuneCount()})
	check(s.Frame())
	m.openPasswordSelection(tab)
	check(s.Frame())
	if !strings.Contains(v.pendingSource, "sample-editor") {
		t.Fatal("explicit selection did not queue for unlock")
	}
	v.password.SetText(master)
	click("button:解锁")
	if v.pendingSource != "" || v.dialog == nil || len(v.dialog.importRows) != 1 {
		t.Fatal("after unlocking, the selected text was not passed to review")
	}
	m.closePasswordVault()
	if m.passwordVault != nil || m.active != tab {
		t.Fatal("closing the vault did not return to the previous document")
	}
}

func TestPasswordVaultTimingOptions(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.openPasswordVault()
	v := m.passwordVault
	v.password.SetText(testVaultMaster)
	v.confirmation.SetText(testVaultMaster)
	m.submitPasswordVault()
	if v.store == nil {
		t.Fatal("could not create test vault", v.problem)
	}
	data := v.store.data.clone()
	data.Next++
	const secret = "synthetic-clipboard-example"
	data.Entries = []passwordEntry{{ID: data.Next, Group: data.DefaultGroup, Kind: vaultLogin, Name: "示例账户", Account: "example-user", Secret: secret}}
	if !m.savePasswordData(data) {
		t.Fatal("could not save test entry", v.problem)
	}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
		check(s.Frame())
	}
	assertDurations := func(minutes, seconds string) {
		t.Helper()
		idle, err := s.Locate("textfield:闲置锁定时间")
		check(err)
		clipboard, err := s.Locate("textfield:剪贴板清理时间")
		check(err)
		if idle.Value != minutes || clipboard.Value != seconds {
			t.Fatal("expected the saved durations in settings")
		}
	}
	click("button:密码本设置")
	assertDurations("5", "20")
	v.dialog.idleMinutes.SetText("0")
	click("button:完成")
	if v.dialog == nil || !strings.Contains(v.dialog.problem, "分钟") || v.store.data.IdleMinutes != 5 {
		t.Fatal("invalid idle duration changed the settings")
	}
	v.dialog.idleMinutes.SetText("7")
	v.dialog.clipboardSeconds.SetText("9223372037")
	click("button:完成")
	if v.dialog == nil || !strings.Contains(v.dialog.problem, "秒") || v.store.data.ClipboardSeconds != 20 {
		t.Fatal("overflowing clipboard duration changed the settings")
	}
	v.dialog.clipboardSeconds.SetText("3")
	click("button:完成")
	if v.dialog != nil || v.store.data.IdleMinutes != 7 || v.store.data.ClipboardSeconds != 3 {
		t.Fatal("custom durations were not saved")
	}
	lastUse := v.lastUse
	if v.expire(lastUse.Add(7*time.Minute-time.Second)) || !v.expire(lastUse.Add(7*time.Minute)) {
		t.Fatal("vault did not expire at the configured idle threshold")
	}
	v.password.SetText(testVaultMaster)
	check(s.Frame())
	click("button:解锁")
	click("button:复制密码")
	pending := v.clipboardCleanup
	if pending == nil || s.Driver().Clipboard() != secret || !strings.Contains(v.message, "3 秒") {
		t.Fatal("copy did not schedule the custom cleanup")
	}

	click("button:密码本设置")
	assertDurations("7", "3")
	click("switch:闲置自动锁定")
	click("switch:复制后清理剪贴板")
	if _, err := s.Locate("textfield:闲置锁定时间"); err == nil {
		t.Fatal("disabled idle timer still displayed its input")
	}
	click("button:完成")
	if v.dialog != nil || !pending.stopped.Load() || v.clipboardCleanup != nil ||
		!v.store.data.DisableIdleLock || !v.store.data.DisableClipboardCleanup || v.expire(v.lastUse.Add(time.Hour)) {
		t.Fatal("turning off the timers did not cancel their behavior")
	}
	click("button:锁定密码本")
	if s.Driver().Clipboard() != secret || v.store != nil {
		t.Fatal("manual locking cleared a clipboard whose cleanup was turned off")
	}
	v.password.SetText(testVaultMaster)
	click("button:解锁")
	if v.store == nil || !v.store.data.DisableIdleLock || !v.store.data.DisableClipboardCleanup {
		t.Fatal("timer settings did not survive reopening the encrypted vault")
	}
	click("button:复制密码")
	if s.Driver().Clipboard() != secret || v.clipboardCleanup != nil || v.clipboardTimer != nil {
		t.Fatal("copy scheduled cleanup while the setting was off")
	}

	click("button:密码本设置")
	click("switch:闲置自动锁定")
	click("switch:复制后清理剪贴板")
	assertDurations("7", "3")
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "password-timing-settings.png")))
	}
	click("button:完成")
	click("button:复制密码")
	click("button:锁定密码本")
	if s.Driver().Clipboard() != "" {
		t.Fatal("re-enabling clipboard cleanup did not restore clearing on lock")
	}
}

func TestPasswordVaultOptionalMasterPassword(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.sidebarPrefs.PasswordVault = true
	path := passwordVaultPath(m.dataDirectory)
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
		check(s.Frame())
	}
	const master, backupPassword = "测试master2026", "测试backup2026"
	click("button:密码本")
	click("switch:使用主密码")
	if _, err := s.Locate("textfield:主密码"); err == nil {
		t.Fatal("disabled master password still required input at creation")
	}
	click("button:创建密码本")
	v := m.passwordVault
	if v.store == nil || !v.windowsProtected || !passwordVaultUsesWindows(v.store.header) {
		t.Fatal("could not create a Windows-protected vault", v.problem)
	}
	data := v.store.data.clone()
	data.Next++
	const secret = "synthetic-secret-for-mode-switch"
	data.Entries = []passwordEntry{{ID: data.Next, Group: data.DefaultGroup, Kind: vaultLogin, Name: "演示", Account: "sample-user", Secret: secret}}
	if !m.savePasswordData(data) {
		t.Fatal("could not save synthetic entry", v.problem)
	}
	assertEntry := func() {
		t.Helper()
		if v.store == nil {
			t.Fatal("vault did not open", v.problem)
		}
		if entry, ok := v.store.data.entry(data.Next); !ok || entry.Secret != secret || entry.Group != data.DefaultGroup {
			t.Fatal("changing protection lost the saved entry")
		}
	}
	current, err := os.ReadFile(path)
	check(err)
	if !passwordVaultUsesWindows(current) || bytes.Contains(current, []byte(secret)) {
		t.Fatal("Windows mode did not keep credentials encrypted")
	}

	click("button:锁定密码本")
	if _, err := s.Locate("textfield:主密码"); err == nil || v.store != nil {
		t.Fatal("locking Windows mode did not clear memory and offer direct opening")
	}
	click("button:打开密码本")
	assertEntry()

	click("button:密码本设置")
	click("switch:使用主密码")
	v.dialog.master.SetText(master)
	v.dialog.masterConfirmation.SetText(backupPassword)
	click("button:完成")
	if v.dialog == nil || !v.windowsProtected || !strings.Contains(v.dialog.problem, "两次") {
		t.Fatal("mismatched master password changed protection")
	}
	unchanged, err := os.ReadFile(path)
	check(err)
	if !bytes.Equal(current, unchanged) {
		t.Fatal("failed master setup changed the encrypted file")
	}
	v.dialog.masterConfirmation.SetText(master)
	click("button:完成")
	assertEntry()
	if v.dialog != nil || v.windowsProtected || passwordVaultUsesWindows(v.store.header) {
		t.Fatal("enabling master password did not change the protection mode")
	}
	click("button:关闭密码本标签")
	click("button:密码本")
	v = m.passwordVault
	if v.store != nil || v.windowsProtected {
		t.Fatal("opening a master-protected vault did not require the password")
	}
	v.password.SetText(backupPassword)
	click("button:解锁")
	if v.store != nil {
		t.Fatal("opening accepted a different master password")
	}
	v.password.SetText(master)
	click("button:解锁")
	assertEntry()

	click("button:密码本设置")
	click("switch:使用主密码")
	click("button:完成")
	assertEntry()
	if !v.windowsProtected {
		t.Fatal("disabling master password did not switch to Windows protection")
	}
	click("button:关闭密码本标签")
	click("button:密码本")
	v = m.passwordVault
	assertEntry()
	if !v.windowsProtected {
		t.Fatal("direct opening did not restore the Windows protection mode")
	}

	click("button:密码本设置")
	click("button:导出加密备份…")
	click("button:取消")
	if v.dialog == nil || v.dialog.mode != "settings" {
		t.Fatal("cancelling the backup did not return to settings")
	}
	click("button:导出加密备份…")
	v.dialog.master.SetText(backupPassword)
	v.dialog.masterConfirmation.SetText(backupPassword)
	backupPath := filepath.Join(t.TempDir(), "portable.edxvault")
	check(s.Driver().QueueSaveFile(backupPath, nil))
	click("button:导出备份…")
	if v.dialog != nil {
		t.Fatal("could not export portable backup", v.dialog.problem)
	}
	encoded, err := os.ReadFile(backupPath)
	check(err)
	if passwordVaultUsesWindows(encoded) || bytes.Contains(encoded, []byte(secret)) {
		t.Fatal("portable backup did not use password-based encryption")
	}
	if _, err := unlockPasswordVault(encoded, master); !errors.Is(err, errVaultUnlock) {
		t.Fatal("portable backup accepted the previous master password", err)
	}
	restored, err := restorePasswordVault(passwordVaultPath(t.TempDir()), backupPath, backupPassword)
	check(err)
	defer restored.clear()
	if entry, ok := restored.data.entry(data.Next); !ok || entry.Secret != secret {
		t.Fatal("portable backup did not preserve credentials")
	}
}

func TestPasswordVaultDeleteFromContextMenu(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.openPasswordVault()
	v := m.passwordVault
	v.password.SetText(testVaultMaster)
	v.confirmation.SetText(testVaultMaster)
	m.submitPasswordVault()
	if v.store == nil {
		t.Fatal("could not create test vault", v.problem)
	}
	data := v.store.data.clone()
	data.Next = 10
	data.Groups = append(data.Groups, passwordGroup{ID: 2, Name: "项目"},
		passwordGroup{ID: 3, Parent: 2, Name: "测试组"},
		passwordGroup{ID: 4, Parent: 3, Name: "子组"},
		passwordGroup{ID: 5, Name: "其他分组"})
	data.Entries = []passwordEntry{
		{ID: 6, Group: 1, Name: "保留条目", Kind: vaultNote, Note: "keep-example"},
		{ID: 7, Group: 1, Name: "删除目标", Kind: vaultNote, Note: "remove-example"},
		{ID: 8, Group: 3, Name: "测试条目", Kind: vaultNote, Note: "move-example-one"},
		{ID: 9, Group: 4, Name: "子组条目", Kind: vaultNote, Note: "move-example-two"},
		{ID: 10, Group: 5, Name: "其他条目", Kind: vaultNote, Note: "unaffected-example"},
	}
	if !m.savePasswordData(data) {
		t.Fatal("could not save synthetic groups and entries", v.problem)
	}
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		check(s.ClickLabel(label))
		check(s.Frame())
	}
	openDelete := func(label, action string) {
		t.Helper()
		check(s.RightClickLabel(label))
		click("menuitem:" + action + "…")
	}
	click("默认分组")
	openDelete("button:删除目标", "删除条目")
	if _, ok := v.store.data.entry(7); !ok || !s.HasText("删除「删除目标」？") {
		t.Fatal("the right-clicked entry was deleted before confirmation or not identified")
	}
	click("button:取消")
	if _, ok := v.store.data.entry(7); !ok {
		t.Fatal("cancelling did not keep the entry")
	}
	openDelete("button:删除目标", "删除条目")
	click("button:确认删除")
	if _, ok := v.store.data.entry(7); ok || v.selected != 6 {
		t.Fatal("confirmed deletion did not remove only the right-clicked entry")
	}
	click("button:编辑条目")
	click("button:删除条目")
	click("button:取消")
	if v.dialog == nil || v.dialog.mode != "entry" || v.dialog.confirmDelete {
		t.Fatal("cancelling deletion from edit did not return to the entry form")
	}
	click("button:取消")

	click("子组")
	check(s.RightClickLabel("测试组"))
	click("menuitem:重命名")
	if v.dialog == nil || v.dialog.id != 3 || v.dialog.name.Text() != "测试组" {
		t.Fatal("rename did not target the right-clicked group")
	}
	check(s.Type("测试环境"))
	click("button:保存分组")
	entry, _ := v.store.data.entry(8)
	if group := v.store.data.group(3); group.Name != "测试环境" || group.Parent != 2 ||
		v.store.data.group(4).Parent != 3 || entry.Group != 3 {
		t.Fatal("renaming changed the group hierarchy or entry assignment")
	}
	click("子组")
	openDelete("测试环境", "删除分组")
	if !s.HasText("同时删除 1 个子分组。2 个条目将移到「项目」。") {
		t.Fatal("the confirmation did not describe the child groups and entry destination")
	}
	check(s.Keys("escape"))
	if v.dialog != nil || v.store.data.group(3).ID == 0 || v.store.data.group(4).ID == 0 {
		t.Fatal("dismissing the confirmation changed the group tree")
	}
	openDelete("测试环境", "删除分组")
	click("button:确认删除")
	if v.group != 2 || v.store.data.group(3).ID != 0 || v.store.data.group(4).ID != 0 {
		t.Fatal("the confirmed group deletion did not remove the branch and select its parent")
	}
	openDelete("默认分组", "删除分组")
	click("button:确认删除")
	encoded, err := os.ReadFile(passwordVaultPath(m.dataDirectory))
	check(err)
	reopened, err := unlockPasswordVault(encoded, testVaultMaster)
	check(err)
	defer reopened.clear()
	if reopened.data.DefaultGroup != 2 || len(reopened.data.Entries) != 4 {
		t.Fatal("default group deletion did not preserve the remaining entries")
	}
	for id, group := range map[int]int{6: 2, 8: 2, 9: 2, 10: 5} {
		if entry, ok := reopened.data.entry(id); !ok || entry.Group != group {
			t.Fatalf("entry %d was not preserved in group %d", id, group)
		}
	}
}

func TestPasswordVaultSidebarNavigation(t *testing.T) {
	m := newEditorApp()
	m.dataDirectory = t.TempDir()
	m.sidebarPrefs.PasswordVault, m.sidebarPrefs.Templates, m.translationEnabled = true, true, true
	s := openTestSession(t, m)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	click := func(label string) {
		t.Helper()
		if !strings.HasPrefix(label, "id:") {
			label = "button:" + label
		}
		check(s.ClickLabel(label))
		check(s.Frame())
	}
	check(s.Resize(1280, 820))
	rail, err := s.Locate("button:密码本")
	check(err)
	railSelector := "id:" + s.Snapshot().Tree.Text(rail.ID)
	click(railSelector)
	if !m.vaultActive() {
		t.Fatal("sidebar did not open the password vault")
	}
	opened := m.passwordVault
	label, err := s.Locate("button:密码本")
	check(err)
	const tabSelector = "id:vault-tab/select"
	closeButton, err := s.Locate("button:关闭密码本标签")
	check(err)
	if gap := closeButton.Bounds.X - (label.Bounds.X + label.Bounds.Width); gap < 0 || gap > 2 || closeButton.Bounds.Y != label.Bounds.Y {
		t.Fatal("the vault title and its close button are not adjacent")
	}
	if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
		check(s.Screenshot(filepath.Join(output, "password-vault-tab-active.png")))
	}
	click(tabSelector)
	if m.vaultActive() || m.passwordVault != opened {
		t.Fatal("clicking the title did not return to the editor while keeping the vault tab")
	}
	click(tabSelector)
	if !m.vaultActive() || m.passwordVault != opened {
		t.Fatal("clicking the inactive title did not reopen the same vault tab")
	}
	click("关闭密码本标签")
	if m.vaultActive() || m.passwordVault != nil {
		t.Fatal("the close button did not remove the vault tab")
	}
	click(railSelector)
	click("关闭密码本")
	if m.passwordVault != nil {
		t.Fatal("clicking the active sidebar key did not close the vault")
	}

	click(railSelector)
	vault := m.passwordVault
	vault.password.SetText(testVaultMaster)
	vault.confirmation.SetText(testVaultMaster)
	click("创建密码本")
	if vault.store == nil {
		t.Fatal("could not create test vault", vault.problem)
	}
	for _, tool := range []struct{ button, visible string }{
		{"AI 助手", "新对话"}, {"翻译", "关闭翻译"}, {"模板库", "关闭模板库"},
		{"Git", "收起底部面板"}, {"终端", "收起底部面板"},
	} {
		for range 2 {
			if !m.vaultActive() {
				click(railSelector)
			}
			click(tool.button)
			if m.vaultActive() || m.passwordVault != vault || vault.store != nil {
				t.Fatalf("%s did not lock the vault and return to the editor", tool.button)
			}
			if _, err := s.Locate("button:" + tool.visible); err != nil {
				t.Fatalf("%s did not show its panel: %v", tool.button, err)
			}
			if (tool.button == "Git" || tool.button == "终端") && m.tools.active != tool.button {
				t.Fatalf("selected %s but opened %q", tool.button, m.tools.active)
			}
		}
	}
	click("收起底部面板")
	click(tabSelector)
	if !m.vaultActive() || !s.HasText("密码本已锁定") {
		t.Fatal("the inactive password tab did not reopen at its lock screen")
	}
	for _, label := range []string{"保存文件", "查找内容", "自动折行"} {
		hit, err := s.Locate("button:" + label)
		check(err)
		if !hit.Disabled {
			t.Fatalf("%s became enabled without a document", label)
		}
	}
	click("设置")
	if !m.appSettings.open || m.vaultActive() || m.passwordVault != vault {
		t.Fatal("settings did not switch away from the password vault")
	}
	click("关闭设置页")
	if m.vaultActive() {
		t.Fatal("closing settings unexpectedly returned to the password vault")
	}

	m.newDocument()
	tab := m.active
	const content = "侧栏切换测试"
	tab.source.replace(content)
	check(s.Frame())
	click(railSelector)
	vault.password.SetText(testVaultMaster)
	click("解锁")
	if vault.store == nil {
		t.Fatal("could not unlock the vault before switching tabs")
	}
	click(tabSelector)
	if m.active != tab || m.vaultActive() || m.passwordVault != vault || vault.store != nil || tab.source.text.String() != content {
		t.Fatal("clicking the vault title did not lock it and return to the last document")
	}
	click(tabSelector)
	wrapped := tab.source.view.Snapshot.Wrap
	click("自动折行")
	if m.vaultActive() || m.active != tab || tab.source.view.Snapshot.Wrap == wrapped {
		t.Fatal("word wrap did not act on the last document")
	}
	click(railSelector)
	click("查找内容")
	if m.vaultActive() || !tab.search.open {
		t.Fatal("document search did not open from the password vault")
	}
	click("关闭搜索")
	click(railSelector)
	path := filepath.Join(t.TempDir(), "saved.txt")
	check(s.Driver().QueueSaveFile(path, nil))
	click("保存文件")
	saved, err := os.ReadFile(path)
	check(err)
	if m.vaultActive() || tab.doc.path != path || string(saved) != content {
		t.Fatal("save did not act on the last document")
	}
}

func TestNativePasswordVaultSessionLock(t *testing.T) {
	if os.Getenv("VIVA_NATIVE_SMOKE") != "1" {
		t.Skip("native session notification test is opt-in")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	directory := filepath.Join(t.TempDir(), "vault-lock")
	var locked bool
	stop, watch, err := listenForEditorActivation(directory, func([]string) bool { return false }, func() { locked = true })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if !watch() {
		t.Fatal("Windows session notification registration failed")
	}
	title := activationWindowTitle(directory)
	handle, _, _ := findWindowEx.Call(messageOnlyWindow, 0, uintptr(unsafe.Pointer(activationClassName)), uintptr(unsafe.Pointer(title)))
	if handle == 0 {
		t.Fatal("activation window was not created")
	}
	editorUser.NewProc("SendMessageW").Call(handle, 0x02B1, 7, 0)
	if !locked {
		t.Fatal("Windows lock notification did not reach the registered handler")
	}
}

func TestPasswordVaultArchiveRestore(t *testing.T) {
	const oldPassword, currentPassword = "archive-demo-master", "current-demo-master"
	for _, scenario := range []struct {
		name          string
		merge, locked bool
	}{
		{name: "仅恢复"},
		{name: "在设置中恢复并合并", merge: true},
		{name: "在锁定页恢复并合并", merge: true, locked: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			m := newEditorApp()
			m.dataDirectory = t.TempDir()
			path := passwordVaultPath(m.dataDirectory)
			vault, err := createPasswordVault(oldPassword)
			check(err)
			original := vault.data.clone()
			original.Groups = append(original.Groups, passwordGroup{ID: 2, Parent: 1, Name: "线上"})
			oldEntry := passwordEntry{ID: 3, Group: 2, Kind: vaultLogin, Name: "控制台",
				Site: "https://example.test", Account: "sample-user", Secret: "old-sample-secret"}
			original.Entries = append(original.Entries, oldEntry)
			original.Next = 3
			check(vault.save(path, original))
			vault.clear()
			originalEncrypted, err := os.ReadFile(path)
			check(err)

			m.openPasswordVault()
			s := openTestSession(t, m)
			check(s.Resize(1080, 720))
			click := func(label string) { t.Helper(); check(s.ClickLabel(label)); check(s.Frame()) }
			v := m.passwordVault
			if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" && scenario.locked {
				check(s.Screenshot(filepath.Join(output, "password-vault-forgot-master.png")))
			}
			click("button:忘记主密码？创建新密码本")
			click("button:取消")
			if v.dialog != nil || len(v.archives) != 0 {
				t.Fatal("cancelling created an archive")
			}
			click("button:忘记主密码？创建新密码本")
			click("button:归档并继续")
			if v.exists || v.store != nil || len(v.archives) != 1 || !s.HasText("创建本机密码本") {
				t.Fatal("confirming did not open the empty vault creation screen")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("the active path was not freed after archiving")
			}
			oldArchive := filepath.Join(m.dataDirectory, passwordVaultArchives, v.archives[0].name)
			archiveBytes, err := os.ReadFile(oldArchive)
			check(err)
			if !bytes.Equal(archiveBytes, originalEncrypted) {
				t.Fatal("archiving changed the old vault ciphertext")
			}

			if scenario.locked {
				v.password.SetText(currentPassword)
				v.confirmation.SetText(currentPassword)
				check(s.Frame())
			} else {
				click("switch:使用主密码")
			}
			click("button:创建密码本")
			if v.store == nil || v.windowsProtected == scenario.locked || len(v.store.data.Entries) != 0 {
				t.Fatal("could not start an independent empty vault")
			}
			current := v.store.data.clone()
			current.Groups = append(current.Groups, passwordGroup{ID: 2, Parent: 1, Name: "线上"},
				passwordGroup{ID: 3, Parent: 2, Name: "子分组"})
			same := oldEntry
			same.ID, same.Favorite = 4, true
			conflict := oldEntry
			conflict.ID, conflict.Secret = 5, "new-sample-secret"
			child := passwordEntry{ID: 6, Group: 3, Kind: vaultNote, Name: "新备注", Note: "example note"}
			current.Entries = append(current.Entries, same, conflict, child)
			current.Next = 6
			check(v.store.save(path, current))
			currentEncrypted, err := os.ReadFile(path)
			check(err)

			if scenario.locked {
				click("button:锁定密码本")
			} else {
				click("button:密码本设置")
			}
			click("button:恢复旧密码本…")
			action := "恢复密码本"
			if scenario.merge {
				click("switch:合并当前密码本的分组和条目")
				action = "恢复并合并"
			}
			if scenario.locked {
				if output := os.Getenv("VIVA_PREVIEW_DIR"); output != "" {
					check(s.Screenshot(filepath.Join(output, "password-vault-restore-and-merge.png")))
				}
				v.dialog.currentMaster.SetText(currentPassword)
			}
			v.dialog.master.SetText("wrong-sample-password")
			check(s.Frame())
			click("button:" + action)
			if v.dialog == nil || v.dialog.problem == "" || len(v.archives) != 1 {
				t.Fatal("a wrong master password changed the active vault or its archives")
			}
			unchanged, err := os.ReadFile(path)
			check(err)
			if !bytes.Equal(unchanged, currentEncrypted) {
				t.Fatal("a failed restore changed the active vault on disk")
			}
			if scenario.locked {
				v.dialog.master.SetText(oldPassword)
				v.dialog.currentMaster.SetText("wrong-current-master")
				check(s.Frame())
				click("button:" + action)
				if v.dialog == nil || !strings.Contains(v.dialog.problem, "当前密码本") || len(v.archives) != 1 {
					t.Fatal("merge accepted an incorrect current vault password")
				}
				v.dialog.currentMaster.SetText(currentPassword)
			}
			v.dialog.master.SetText(oldPassword)
			check(s.Frame())
			click("button:" + action)
			if v.dialog != nil || v.store == nil || v.windowsProtected || len(v.archives) != 2 {
				t.Fatal("the old vault did not become active with the current vault archived")
			}
			active, err := os.ReadFile(path)
			check(err)
			reopened, err := unlockPasswordVault(active, oldPassword)
			check(err)
			defer reopened.clear()
			archivedCurrent := filepath.Join(m.dataDirectory, passwordVaultArchives, v.archives[0].name)
			retained, err := os.ReadFile(archivedCurrent)
			check(err)
			if !bytes.Equal(retained, currentEncrypted) {
				t.Fatal("the current vault was not retained verbatim")
			}
			entries := reopened.data.Entries
			if !scenario.merge {
				if len(entries) != 1 || entries[0].Secret != oldEntry.Secret || entries[0].Favorite {
					t.Fatal("restore-only incorporated current entries")
				}
				click("button:密码本设置")
				click("button:恢复旧密码本…")
				click("button:恢复密码本")
				if v.store == nil || !v.windowsProtected || len(v.store.data.Entries) != 3 {
					t.Fatal("the archived Windows-protected vault could not be restored")
				}
				return
			}
			if len(entries) != 3 || !entries[0].Favorite || entries[0].Secret != oldEntry.Secret ||
				entries[1].Secret != conflict.Secret || entries[1].Group != entries[0].Group ||
				reopened.data.group(entries[2].Group).Parent != entries[0].Group {
				t.Fatal("merge did not preserve both credentials, deduplicate the identical one, and retain nested groups")
			}
		})
	}
}
