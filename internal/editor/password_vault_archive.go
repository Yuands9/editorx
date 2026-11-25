package editor

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/vivaui/viva/ui"
)

const (
	passwordVaultArchives = "password-vault-archives"
	passwordArchiveTime   = "20060102T150405.000000000Z"
)

type passwordVaultArchive struct {
	name, label      string
	windowsProtected bool
}

func newPasswordArchivePath(activePath string) string {
	name := time.Now().UTC().Format(passwordArchiveTime) + "-" + rand.Text() + ".edxvault"
	return filepath.Join(filepath.Dir(activePath), passwordVaultArchives, name)
}

func readPasswordVaultArchives(activePath string) []passwordVaultArchive {
	directory := filepath.Join(filepath.Dir(activePath), passwordVaultArchives)
	files, err := os.ReadDir(directory)
	if err != nil {
		return nil
	}
	archives := make([]passwordVaultArchive, 0, len(files))
	labels := make(map[string]int)
	for _, entry := range slices.Backward(files) {
		name := entry.Name()
		if !entry.Type().IsRegular() || filepath.Ext(name) != ".edxvault" {
			continue
		}
		stamp, _, _ := strings.Cut(name, "-")
		when, err := time.Parse(passwordArchiveTime, stamp)
		if err != nil {
			continue
		}
		file, err := os.Open(filepath.Join(directory, name))
		if err != nil {
			continue
		}
		var prefix [len(passwordVaultMagic)]byte
		_, err = io.ReadFull(file, prefix[:])
		file.Close()
		label := when.Local().Format("2006-01-02 15:04:05")
		labels[label]++
		if labels[label] > 1 {
			label = fmt.Sprintf("%s（%d）", label, labels[label])
		}
		archives = append(archives, passwordVaultArchive{name, label, err == nil && passwordVaultUsesWindows(prefix[:])})
	}
	return archives
}

func (m *editorApp) openPasswordArchiveRestore() {
	v := m.passwordVault
	v.archives = readPasswordVaultArchives(passwordVaultPath(m.dataDirectory))
	if len(v.archives) == 0 {
		v.problem = "没有可恢复的旧密码本"
		return
	}
	v.dialog = &passwordVaultDialog{open: true, mode: passwordArchiveRestoreMode, archive: v.archives[0],
		master: ui.Field{Secure: true}, currentMaster: ui.Field{Secure: true}, returnToSettings: v.dialog}
	m.vaultTouch()
}

func (m *editorApp) passwordArchiveModal(c ui.C, v *passwordVaultView, d *passwordVaultDialog) {
	reset := d.mode == passwordArchiveResetMode
	title := "恢复旧密码本"
	if reset {
		title = "创建新密码本"
	}
	closeDialog := func() {
		v.dialog = d.returnToSettings
		m.vaultTouch()
	}
	needsCurrentPassword := func() bool { return d.mergeCurrent && v.exists && v.store == nil && !v.windowsProtected }
	if ui.ModalWith(c, &d.open, title, ui.ModalSpec{Width: 475}, func() {
		ui.Column(c).FillWidth().Gap(9).Children(func() {
			if reset {
				ui.Wrap(c, "当前密码本会以原密码加密保留。日后想起密码，可通过「恢复旧密码本」重新打开。").FillWidth()
			} else {
				if ui.Select(c.Part("archive-file"), &d.archive, v.archives, func(archive passwordVaultArchive) string { return archive.label }).
					Label("归档时间").FillWidth().Height(34).Changed() {
					d.master, d.problem = ui.Field{Secure: true}, ""
				}
				if !d.archive.windowsProtected {
					m.passwordInput(c.Part("archive-password"), &d.master, "旧密码本主密码", "输入旧密码本主密码", 36, false).AutoFocus()
				}
				if v.exists {
					if settingsSwitchRow(c.Part("merge-current"), "合并当前密码本的分组和条目", &d.mergeCurrent) {
						d.currentMaster, d.problem = ui.Field{Secure: true}, ""
					}
					if needsCurrentPassword() {
						m.passwordInput(c.Part("current-master"), &d.currentMaster, "当前密码本主密码", "输入当前密码本主密码", 36, false)
					}
					ui.Wrap(c, "恢复后，当前密码本会加密归档。").FillWidth().Muted().FontSize(12)
				}
			}
			if d.problem != "" {
				ui.Wrap(c, d.problem).FillWidth().Color(m.theme.Danger).FontSize(12)
			}
		})
	}, func() {
		if ui.Button(c, "取消").Ghost().Clicked() {
			c.After(closeDialog)
		}
		if reset {
			if ui.Button(c, "归档并继续").Clicked() {
				c.After(m.archiveCurrentPasswordVault)
			}
			return
		}
		ready := d.archive.name != "" && (d.archive.windowsProtected || d.master.Text() != "") &&
			(!needsCurrentPassword() || d.currentMaster.Text() != "")
		label := "恢复密码本"
		if d.mergeCurrent {
			label = "恢复并合并"
		}
		if ui.Button(c, label).Disabled(!ready).Clicked() {
			c.After(m.restorePasswordArchive)
		}
	}) {
		closeDialog()
	}
}

func (m *editorApp) archiveCurrentPasswordVault() {
	v, path := m.passwordVault, passwordVaultPath(m.dataDirectory)
	d := v.dialog
	passwordVaultFileLock.Lock()
	archive := newPasswordArchivePath(path)
	err := os.MkdirAll(filepath.Dir(archive), 0700)
	if err == nil {
		err = os.Rename(path, archive)
	}
	passwordVaultFileLock.Unlock()
	if err != nil {
		d.problem = fmt.Sprintf("无法归档旧密码本：%v", err)
		return
	}
	pending := v.pendingSource
	v.lock()
	v.exists, v.windowsProtected, v.useMaster = false, false, true
	v.group, v.selected, v.favorites = passwordVaultAllGroups, 0, false
	v.pendingSource, v.archives = pending, readPasswordVaultArchives(path)
	m.lockOtherPasswordVaults()
}

func (m *editorApp) restorePasswordArchive() {
	v, path := m.passwordVault, passwordVaultPath(m.dataDirectory)
	d := v.dialog
	if passwordComposing(&d.master, &d.currentMaster) {
		return
	}
	merge := v.exists && d.mergeCurrent
	password, currentPassword := d.master.Text(), d.currentMaster.Text()
	d.master, d.currentMaster = ui.Field{Secure: true}, ui.Field{Secure: true}
	archivePath := filepath.Join(m.dataDirectory, passwordVaultArchives, d.archive.name)
	encoded, err := readTextAsset(archivePath, passwordVaultBytes)
	if err != nil {
		d.problem = fmt.Sprintf("无法读取旧密码本：%v", err)
		return
	}
	restored, err := unlockPasswordVault(encoded, password)
	if err != nil {
		d.problem = err.Error()
		return
	}
	data := restored.data
	switch {
	case v.store != nil:
		restored.revision = v.store.revision
		if merge {
			data = mergePasswordVaults(data, v.store.data)
		}
	case v.exists:
		var current []byte
		current, err = readTextAsset(path, passwordVaultBytes)
		if err == nil {
			restored.revision = sha256.Sum256(current)
		}
		if err == nil && merge {
			var currentVault *unlockedPasswordVault
			currentVault, err = unlockPasswordVault(current, currentPassword)
			if err == nil {
				data = mergePasswordVaults(data, currentVault.data)
				currentVault.clear()
			} else {
				err = fmt.Errorf("无法解锁当前密码本：%w", err)
			}
		}
	default:
		restored.revision = [32]byte{}
	}
	if err == nil {
		err = restored.saveWithArchive(path, data, v.exists)
	}
	if err != nil {
		restored.clear()
		d.problem = err.Error()
		return
	}

	pending := v.pendingSource
	v.lock()
	v.store, v.exists = restored, true
	v.windowsProtected = passwordVaultUsesWindows(restored.header)
	v.archives = readPasswordVaultArchives(path)
	v.group, v.selected, v.favorites = passwordVaultAllGroups, 0, false
	v.message = "已恢复旧密码本"
	if merge {
		v.message = "已恢复旧密码本并合并当前内容"
	}
	m.lockOtherPasswordVaults()
	m.vaultTouch()
	if pending != "" {
		m.openPasswordImport(pending)
	}
}

func mergePasswordVaults(older, current passwordVaultData) passwordVaultData {
	merged := older.clone()
	groupIDs := make(map[int]int, len(current.Groups))
	children := current.groupsByParent()
	var mergeGroups func(int, int)
	mergeGroups = func(sourceParent, destinationParent int) {
		for _, group := range children[sourceParent] {
			index := slices.IndexFunc(merged.Groups, func(existing passwordGroup) bool {
				return existing.Parent == destinationParent && strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(group.Name))
			})
			if index < 0 {
				merged.Next++
				merged.Groups = append(merged.Groups, passwordGroup{
					ID: merged.Next, Parent: destinationParent, Name: group.Name, Production: group.Production,
				})
				index = len(merged.Groups) - 1
			}
			destination := merged.Groups[index].ID
			merged.Groups[index].Production = merged.Groups[index].Production || group.Production
			groupIDs[group.ID] = destination
			mergeGroups(group.ID, destination)
		}
	}
	mergeGroups(passwordVaultNoGroup, passwordVaultNoGroup)

	type entryKey struct {
		group, tagCount                         int
		name, kind, site, account, secret, note string
		tags                                    string
	}
	keyFor := func(entry passwordEntry) entryKey {
		return entryKey{entry.Group, len(entry.Tags), entry.Name, entry.Kind, entry.Site, entry.Account, entry.Secret, entry.Note, strings.Join(entry.Tags, "\x00")}
	}
	existing := make(map[entryKey]int, len(older.Entries)+len(current.Entries))
	for index, entry := range merged.Entries {
		existing[keyFor(entry)] = index
	}
	for _, entry := range current.Entries {
		entry.Group = groupIDs[entry.Group]
		key := keyFor(entry)
		index, duplicate := existing[key]
		if duplicate {
			merged.Entries[index].Favorite = merged.Entries[index].Favorite || entry.Favorite
			continue
		}
		merged.Next++
		entry.ID = merged.Next
		existing[key] = len(merged.Entries)
		merged.Entries = append(merged.Entries, entry)
	}
	return merged
}
