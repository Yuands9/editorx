package editor

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
)

type passwordVaultView struct {
	active, exists, favorites, revealed, focusSearch bool
	useMaster, windowsProtected                      bool
	store                                            *unlockedPasswordVault
	password, confirmation, search                   ui.Field
	resultList                                       ui.ListState
	groupTree                                        ui.TreeState
	closedGroups                                     map[int]bool
	group, selected                                  int
	collapsed                                        map[string]bool
	message, problem, backupSource, pendingSource    string
	lastUse                                          time.Time
	idleTimer, clipboardTimer                        *time.Timer
	clipboardCleanup                                 *passwordClipboardCleanup
	dialog                                           *passwordVaultDialog
	archives                                         []passwordVaultArchive
}

const (
	passwordDeleteEntryMode    = "delete-entry"
	passwordDeleteGroupMode    = "delete-group"
	passwordBackupExportMode   = "export-backup"
	passwordArchiveResetMode   = "archive-reset"
	passwordArchiveRestoreMode = "archive-restore"
)

type passwordVaultDialog struct {
	open, confirmDelete, useMaster bool
	master, masterConfirmation     ui.Field
	returnToSettings               *passwordVaultDialog
	archive                        passwordVaultArchive
	mergeCurrent                   bool
	currentMaster                  ui.Field
	mode, problem                  string
	id, group, parent              int
	idleLock, clearClipboard       bool
	idleMinutes, clipboardSeconds  ui.Field

	kind                                            string
	name, account, site, secret, note, tags, source ui.Field
	importText                                      string
	importRows                                      []passwordImportDraft
}

type passwordImportDraft struct {
	name, site, account, secret ui.Field
	source                      string
	included                    bool
}

func (m *editorApp) vaultActive() bool { return m.passwordVault != nil && m.passwordVault.active }

func (m *editorApp) togglePasswordVault() {
	if m.vaultActive() {
		m.closePasswordVault()
		return
	}
	m.openPasswordVault()
}

func (m *editorApp) openPasswordVault() {
	if m.appSettings.open {
		m.closeSettings()
	}
	if m.passwordVault == nil {
		m.passwordVault = &passwordVaultView{group: passwordVaultAllGroups, collapsed: make(map[string]bool), useMaster: true}
		m.passwordVault.lock()
	}
	v := m.passwordVault
	if v.active {
		return
	}
	v.active = true
	path := passwordVaultPath(m.dataDirectory)
	v.readProtection(path)
	v.archives = readPasswordVaultArchives(path)
	if v.windowsProtected {
		m.submitPasswordVault()
	}
}

func (v *passwordVaultView) readProtection(path string) {
	v.problem, v.windowsProtected = "", false
	file, err := os.Open(path)
	v.exists = !errors.Is(err, os.ErrNotExist)
	if err != nil {
		if v.exists {
			v.problem = err.Error()
		}
		return
	}
	defer file.Close()
	var prefix [len(passwordVaultMagic)]byte
	if _, err := io.ReadFull(file, prefix[:]); err != nil {
		v.problem = "密码库格式无效或版本不受支持"
		return
	}
	v.windowsProtected = passwordVaultUsesWindows(prefix[:])
}

func (m *editorApp) closePasswordVault() {
	if v := m.passwordVault; v != nil {
		v.lock()
		m.passwordVault = nil
	}
	m.focusActiveSource()
}

func (m *editorApp) leavePasswordVault() bool {
	v := m.passwordVault
	if v == nil || !v.active {
		return false
	}
	v.lock()
	v.active = false
	return true
}

func (v *passwordVaultView) lock() {
	if v.idleTimer != nil {
		v.idleTimer.Stop()
		v.idleTimer = nil
	}
	v.releaseClipboard()
	if v.store != nil {
		v.store.clear()
	}
	v.store = nil
	v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
	v.search = ui.Field{}
	v.resultList = ui.ListState{}
	v.groupTree = ui.TreeState{}
	v.closedGroups = make(map[int]bool)
	v.pendingSource, v.backupSource, v.problem, v.message = "", "", "", ""
	v.dialog = nil
	v.revealed, v.focusSearch = false, false
}

func (m *editorApp) vaultTouch() {
	v := m.passwordVault
	if v == nil || v.store == nil {
		return
	}
	v.lastUse = time.Now()
	if v.idleTimer != nil {
		v.idleTimer.Stop()
		v.idleTimer = nil
	}
	if m.post == nil || v.store.data.DisableIdleLock {
		return
	}
	delay := time.Duration(v.store.data.IdleMinutes) * time.Minute
	v.idleTimer = time.AfterFunc(delay, func() {
		m.post(func() {
			if m.passwordVault == v && v.expire(time.Now()) {
				m.invalidator.Invalidate()
			}
		})
	})
}

func (v *passwordVaultView) expire(now time.Time) bool {
	if v.store == nil || v.store.data.DisableIdleLock || now.Sub(v.lastUse) < time.Duration(v.store.data.IdleMinutes)*time.Minute {
		return false
	}
	v.lock()
	return true
}

func (m *editorApp) openPasswordSelection(tab *editorTab) {
	target, err := captureTextTarget(tab, true, passwordImportLimit)
	if err != nil {
		m.fail(err)
		return
	}
	m.openPasswordVault()
	v := m.passwordVault
	if v.store == nil {
		v.pendingSource = strings.Clone(target.selected)
		return
	}
	m.openPasswordImport(target.selected)
}

func (m *editorApp) submitPasswordVault() {
	v := m.passwordVault
	if v == nil || v.store != nil || passwordComposing(&v.password, &v.confirmation) {
		return
	}
	if m.nativeWindow != nil && m.desktop != nil && (m.desktop.watchPasswordSession == nil || !m.desktop.watchPasswordSession()) {
		v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
		v.problem = "暂时无法监听 Windows 锁屏，请稍后重试"
		return
	}
	password := v.password.Text()
	confirmation := v.confirmation.Text()
	v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
	path := passwordVaultPath(m.dataDirectory)
	var unlocked *unlockedPasswordVault
	var err error
	switch {
	case v.backupSource != "" && !v.exists:
		unlocked, err = restorePasswordVault(path, v.backupSource, password)
	case v.exists:
		var encoded []byte
		encoded, err = readTextAsset(path, passwordVaultBytes)
		if err == nil {
			v.windowsProtected = passwordVaultUsesWindows(encoded)
			unlocked, err = unlockPasswordVault(encoded, password)
		}
	default:
		if v.useMaster {
			unlocked, err = createConfirmedPasswordVault(password, confirmation)
		} else {
			unlocked, err = createWindowsPasswordVault()
		}
		if err == nil {
			err = unlocked.save(path, unlocked.data)
			if err != nil {
				unlocked.clear()
			}
		}
	}
	if err != nil {
		if errors.Is(err, errVaultChanged) {
			v.readProtection(path)
			v.backupSource = ""
		}
		v.problem = err.Error()
		return
	}
	v.store, v.exists, v.problem, v.backupSource = unlocked, true, "", ""
	v.windowsProtected = passwordVaultUsesWindows(unlocked.header)
	m.lockOtherPasswordVaults()
	if v.group > 0 && unlocked.data.group(v.group).ID == 0 {
		v.group = passwordVaultAllGroups
	}
	m.vaultTouch()
	if source := v.pendingSource; source != "" {
		v.pendingSource = ""
		m.openPasswordImport(source)
	}
}

func (m *editorApp) lockOtherPasswordVaults() {
	if m.desktop == nil {
		return
	}
	for _, window := range m.desktop.store.windows {
		if window != m && window.passwordVault != nil {
			window.passwordVault.lock()
			window.passwordVault.exists = m.passwordVault.exists
			window.passwordVault.windowsProtected = m.passwordVault.windowsProtected
			window.passwordVault.archives = m.passwordVault.archives
			window.invalidator.Invalidate()
		}
	}
}

func createConfirmedPasswordVault(password, confirmation string) (*unlockedPasswordVault, error) {
	if password != confirmation {
		return nil, errors.New("两次输入的密码不同")
	}
	return createPasswordVault(password)
}

func passwordComposing(fields ...*ui.Field) bool {
	for _, field := range fields {
		if editor := field.GetEditor(); editor != nil {
			if _, active := editor.Composition(); active {
				return true
			}
		}
	}
	return false
}

func (m *editorApp) savePasswordData(data passwordVaultData) bool {
	v := m.passwordVault
	if err := v.store.save(passwordVaultPath(m.dataDirectory), data); err != nil {
		if v.dialog != nil {
			v.dialog.problem = err.Error()
		} else {
			v.problem = err.Error()
		}
		return false
	}
	v.problem = ""
	m.vaultTouch()
	return true
}

func (m *editorApp) savePasswordVaultSettings() {
	v := m.passwordVault
	d := v.dialog
	data := v.store.data.clone()
	data.DisableIdleLock, data.DisableClipboardCleanup = !d.idleLock, !d.clearClipboard
	if d.idleLock {
		if passwordComposing(&d.idleMinutes) {
			return
		}
		minutes, err := strconv.Atoi(strings.TrimSpace(d.idleMinutes.Text()))
		if err != nil || !validPasswordVaultDuration(minutes, time.Minute) {
			d.problem = "请输入有效的闲置锁定分钟数，至少 1 分钟"
			return
		}
		data.IdleMinutes = minutes
	}
	if d.clearClipboard {
		if passwordComposing(&d.clipboardSeconds) {
			return
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(d.clipboardSeconds.Text()))
		if err != nil || !validPasswordVaultDuration(seconds, time.Second) {
			d.problem = "请输入有效的剪贴板清理秒数，至少 1 秒"
			return
		}
		data.ClipboardSeconds = seconds
	}
	if d.useMaster == !v.windowsProtected {
		if m.savePasswordData(data) {
			if data.DisableClipboardCleanup {
				v.cancelClipboardCleanup()
			}
			v.dialog = nil
		}
		return
	}
	var next *unlockedPasswordVault
	var err error
	if d.useMaster {
		if passwordComposing(&d.master, &d.masterConfirmation) {
			return
		}
		next, err = createConfirmedPasswordVault(d.master.Text(), d.masterConfirmation.Text())
	} else {
		next, err = createWindowsPasswordVault()
	}
	if err != nil {
		d.problem = err.Error()
		return
	}
	next.revision = v.store.revision
	if err := next.save(passwordVaultPath(m.dataDirectory), data); err != nil {
		next.clear()
		d.problem = err.Error()
		return
	}
	v.store.clear()
	v.store, v.windowsProtected, v.dialog = next, !d.useMaster, nil
	if data.DisableClipboardCleanup {
		v.cancelClipboardCleanup()
	}
	m.lockOtherPasswordVaults()
	m.vaultTouch()
}

func (m *editorApp) choosePasswordVaultBackup() {
	v := m.passwordVault
	paths, err := app.OpenFiles(app.OpenFileOptions{Title: "打开加密密码备份", Filters: []app.FileFilter{{Name: "EditorX 密码库", Pattern: "*.edxvault"}}})
	if errors.Is(err, app.ErrCanceled) || err == nil && len(paths) == 0 {
		return
	}
	if err != nil {
		v.problem = err.Error()
		return
	}
	v.backupSource, v.problem = paths[0], ""
	v.password, v.confirmation = ui.Field{Secure: true}, ui.Field{Secure: true}
}

func (m *editorApp) requestPasswordVaultExport() {
	v := m.passwordVault
	if !v.windowsProtected {
		m.exportPasswordVault()
		return
	}
	v.dialog = &passwordVaultDialog{open: true, mode: passwordBackupExportMode, returnToSettings: v.dialog,
		master: ui.Field{Secure: true}, masterConfirmation: ui.Field{Secure: true}}
	m.vaultTouch()
}

func (m *editorApp) exportPasswordVault() {
	v := m.passwordVault
	dialog := v.dialog
	path := passwordVaultPath(m.dataDirectory)
	encoded, err := readTextAsset(path, passwordVaultBytes)
	if err != nil || sha256.Sum256(encoded) != v.store.revision {
		dialog.problem = "无法读取当前加密密码库，请重新解锁后再试"
		return
	}
	if v.windowsProtected {
		if passwordComposing(&dialog.master, &dialog.masterConfirmation) {
			return
		}
		portable, err := createConfirmedPasswordVault(dialog.master.Text(), dialog.masterConfirmation.Text())
		if err != nil {
			dialog.problem = err.Error()
			return
		}
		defer portable.clear()
		encoded, err = portable.encode(v.store.data)
		if err != nil {
			dialog.problem = err.Error()
			return
		}
	}
	backup, err := app.SaveFile(app.SaveFileOptions{Title: "导出加密密码备份", SuggestedName: "EditorX-password-backup.edxvault", Filters: []app.FileFilter{{Name: "EditorX 密码库", Pattern: "*.edxvault"}}})
	if errors.Is(err, app.ErrCanceled) {
		return
	}
	if v.store == nil || v.dialog != dialog {
		return
	}
	if err == nil {
		if !strings.EqualFold(filepath.Ext(backup), ".edxvault") {
			backup += ".edxvault"
		}
		absolute, pathErr := filepath.Abs(backup)
		if pathErr != nil {
			err = pathErr
		} else if strings.EqualFold(absolute, path) {
			err = errors.New("请选择密码库之外的备份文件位置")
		} else {
			err = writeTextAsset(absolute, encoded, passwordVaultBytes)
		}
	}
	if err != nil {
		dialog.problem = err.Error()
		return
	}
	dialog.problem = ""
	v.message = "已导出加密备份"
	v.dialog = nil
	m.vaultTouch()
}

type passwordClipboardCleanup struct {
	clear   func() bool
	stopped atomic.Bool
}

func (cleanup *passwordClipboardCleanup) try(attempts int) {
	if cleanup.stopped.Load() {
		return
	}
	if cleanup.clear() {
		cleanup.stopped.Store(true)
		return
	}
	if attempts > 0 {
		time.AfterFunc(150*time.Millisecond, func() { cleanup.try(attempts - 1) })
	}
}

func (v *passwordVaultView) takeClipboardCleanup() *passwordClipboardCleanup {
	if v.clipboardTimer != nil {
		v.clipboardTimer.Stop()
		v.clipboardTimer = nil
	}
	cleanup := v.clipboardCleanup
	v.clipboardCleanup = nil
	return cleanup
}

func (v *passwordVaultView) cancelClipboardCleanup() {
	if cleanup := v.takeClipboardCleanup(); cleanup != nil {
		cleanup.stopped.Store(true)
		v.message = ""
	}
}

func (v *passwordVaultView) releaseClipboard() {
	if cleanup := v.takeClipboardCleanup(); cleanup != nil {
		cleanup.try(20)
	}
}

func (m *editorApp) copyPasswordField(value, label string) {
	v := m.passwordVault
	v.releaseClipboard()
	clearClipboard, err := setPasswordClipboard(value, m.nativeWindow != nil)
	if err != nil {
		v.problem = err.Error()
		return
	}
	v.problem, v.message = "", "已复制"+label
	if !v.store.data.DisableClipboardCleanup {
		cleanup := &passwordClipboardCleanup{clear: clearClipboard}
		v.clipboardCleanup = cleanup
		seconds := v.store.data.ClipboardSeconds
		if m.nativeWindow != nil {
			v.clipboardTimer = time.AfterFunc(time.Duration(seconds)*time.Second, func() { cleanup.try(20) })
		}
		v.message = fmt.Sprintf("已复制%s，%d 秒后清理剪贴板", label, seconds)
	}
	m.vaultTouch()
}

func (m *editorApp) openPasswordEntry(entry passwordEntry) {
	v := m.passwordVault
	d := &passwordVaultDialog{open: true, mode: "entry", id: entry.ID, group: entry.Group, kind: entry.Kind, secret: ui.Field{Secure: true}}
	if d.kind == "" {
		d.kind = vaultLogin
	}
	d.name.SetText(entry.Name)
	d.account.SetText(entry.Account)
	d.site.SetText(entry.Site)
	d.secret.SetText(entry.Secret)
	d.note.SetText(entry.Note)
	d.tags.SetText(strings.Join(entry.Tags, "，"))
	v.revealed, v.dialog = false, d
	m.vaultTouch()
}

func (m *editorApp) savePasswordEntry() {
	v := m.passwordVault
	d := v.dialog
	if passwordComposing(&d.name, &d.account, &d.secret, &d.site, &d.tags, &d.note) {
		return
	}
	entry := passwordEntry{ID: d.id, Group: d.group, Name: strings.TrimSpace(d.name.Text()), Kind: d.kind, Note: d.note.Text()}
	if old, ok := v.store.data.entry(entry.ID); ok {
		entry.Favorite = old.Favorite
	}
	if d.kind == vaultLogin {
		entry.Account = strings.TrimSpace(d.account.Text())
	}
	if d.kind != vaultNote {
		entry.Site = strings.TrimSpace(d.site.Text())
		entry.Secret = d.secret.Text()
	}
	for _, tag := range strings.FieldsFunc(d.tags.Text(), func(r rune) bool { return r == ',' || r == '，' }) {
		if tag = strings.TrimSpace(tag); tag != "" {
			entry.Tags = append(entry.Tags, tag)
		}
	}
	if err := validatePasswordEntry(entry); err != nil {
		d.problem = err.Error()
		return
	}
	data := v.store.data.clone()
	if entry.ID == 0 {
		data.Next++
		entry.ID = data.Next
		data.Entries = append([]passwordEntry{entry}, data.Entries...)
	} else {
		for i, item := range data.Entries {
			if item.ID == entry.ID {
				data.Entries[i] = entry
				break
			}
		}
	}
	if m.savePasswordData(data) {
		v.selectSaved(entry)
		v.dialog = nil
	}
}

func (v *passwordVaultView) selectSaved(entry passwordEntry) {
	v.selected, v.revealed, v.favorites = entry.ID, false, false
	if v.group != passwordVaultAllGroups {
		v.selectGroup(entry.Group)
	}
	if site, _ := passwordSite(entry); site != "" {
		delete(v.collapsed, site)
	}
	v.search = ui.Field{}
	v.resultList.ScrollIntoView(0)
}

func (m *editorApp) confirmPasswordDelete(mode string, id int) {
	v := m.passwordVault
	v.dialog = &passwordVaultDialog{open: true, mode: mode, id: id}
	v.revealed = false
	m.vaultTouch()
}

func (m *editorApp) deletePasswordEntry() {
	v := m.passwordVault
	d := v.dialog
	data := v.store.data.clone()
	data.Entries = slices.DeleteFunc(data.Entries, func(entry passwordEntry) bool { return entry.ID == d.id })
	if m.savePasswordData(data) {
		v.revealed = false
		v.dialog = nil
	}
}

func (m *editorApp) deletePasswordGroup() {
	v := m.passwordVault
	data := v.store.data.clone()
	removed, destination := data.groupRemoval(v.dialog.id)
	data.Groups = slices.DeleteFunc(data.Groups, func(group passwordGroup) bool { return removed[group.ID] })
	for i := range data.Entries {
		if removed[data.Entries[i].Group] {
			data.Entries[i].Group = destination
		}
	}
	if removed[data.DefaultGroup] {
		data.DefaultGroup = destination
	}
	if !m.savePasswordData(data) {
		return
	}
	for id := range removed {
		delete(v.closedGroups, id)
	}
	if removed[v.group] {
		v.selectGroup(destination)
	}
	v.revealed = false
	v.dialog = nil
}

func (v *passwordVaultView) entryGroup() int {
	if v.group == passwordVaultAllGroups {
		return v.store.data.DefaultGroup
	}
	return v.group
}

func (v *passwordVaultView) selectGroup(id int) {
	v.group, v.revealed = id, false
	for parent := v.store.data.group(id).Parent; parent != 0; parent = v.store.data.group(parent).Parent {
		delete(v.closedGroups, parent)
	}
	v.resultList.ScrollIntoView(0)
}

func (m *editorApp) openPasswordGroup(id, parent int) {
	v := m.passwordVault
	group := v.store.data.group(id)
	d := &passwordVaultDialog{open: true, mode: "group", id: id, parent: parent}
	if id != 0 {
		d.parent = group.Parent
		d.name.SetText(group.Name)
		d.name.Select(0, d.name.GetEditor().RuneCount())
	}
	v.dialog, v.revealed = d, false
	m.vaultTouch()
}

func (m *editorApp) savePasswordGroup() {
	v := m.passwordVault
	d := v.dialog
	if passwordComposing(&d.name) {
		return
	}
	group := passwordGroup{ID: d.id, Parent: d.parent, Name: strings.TrimSpace(d.name.Text())}
	data := v.store.data.clone()
	if group.ID == 0 {
		data.Next++
		group.ID = data.Next
		data.Groups = append(data.Groups, group)
	} else {
		for i, item := range data.Groups {
			if item.ID == group.ID {
				group.Production = item.Production
				data.Groups[i] = group
				break
			}
		}
	}
	if m.savePasswordData(data) {
		v.selectGroup(group.ID)
		v.favorites = false
		v.search = ui.Field{}
		v.dialog = nil
	}
}

func (m *editorApp) openPasswordImport(source string) {
	v := m.passwordVault
	v.dialog = &passwordVaultDialog{open: true, mode: "import", group: v.entryGroup()}
	v.dialog.source.SetText(strings.Clone(source))
	v.revealed = false
	m.vaultTouch()
}

func (d *passwordVaultDialog) parsePasswordImport() {
	source := d.source.GetEditor().CommittedString()
	if d.importText == source {
		return
	}
	d.importText, d.problem = source, ""
	d.importRows = nil
	d.id = 0
	if len(source) > passwordImportLimit {
		d.problem = "请一次粘贴不超过 64 KiB 的文本"
		return
	}
	candidates := detectPasswordEntries(source)
	if len(candidates) > passwordImportRows {
		d.problem = fmt.Sprintf("一次最多识别 %d 条，请分批粘贴", passwordImportRows)
		return
	}
	for _, candidate := range candidates {
		entry := candidate.entry
		row := passwordImportDraft{source: candidate.source, included: validatePasswordEntry(entry) == nil, secret: ui.Field{Secure: true}}
		row.name.SetText(entry.Name)
		row.site.SetText(entry.Site)
		row.account.SetText(entry.Account)
		row.secret.SetText(entry.Secret)
		d.importRows = append(d.importRows, row)
	}
}

func (row *passwordImportDraft) entry(group int) passwordEntry {
	return passwordEntry{Group: group, Kind: vaultLogin, Name: strings.TrimSpace(row.name.Text()), Account: strings.TrimSpace(row.account.Text()), Site: strings.TrimSpace(row.site.Text()), Secret: row.secret.Text()}
}

func (m *editorApp) savePasswordImport() {
	v := m.passwordVault
	d := v.dialog
	d.parsePasswordImport()
	data := v.store.data.clone()
	var imported []passwordEntry
	for i := range d.importRows {
		row := &d.importRows[i]
		if !row.included {
			continue
		}
		if passwordComposing(&row.name, &row.site, &row.account, &row.secret) {
			return
		}
		entry := row.entry(d.group)
		if err := validatePasswordEntry(entry); err != nil {
			d.id = i
			d.problem = err.Error()
			return
		}
		data.Next++
		entry.ID = data.Next
		imported = append(imported, entry)
	}
	if len(imported) == 0 {
		return
	}
	data.Entries = append(imported, data.Entries...)
	if m.savePasswordData(data) {
		v.selectSaved(imported[0])
		v.dialog = nil
	}
}

func generatePassword() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!#$%&*+-=?"
	size := big.NewInt(int64(len(alphabet)))
	var output [24]byte
	defer clear(output[:])
	for i := range output {
		index, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", err
		}
		output[i] = alphabet[index.Int64()]
	}
	return string(output[:]), nil
}
