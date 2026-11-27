package editor

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	passwordVaultName            = "passwords.edxvault"
	passwordVaultMagic           = "EditorX-PasswordVault\x01"
	passwordVaultWindowsMagic    = "EditorX-PasswordVault\x02"
	passwordVaultMaxWrappedKey   = 4096
	passwordVaultSaltSize        = 16
	passwordVaultRounds          = 600_000
	passwordVaultMinMasterLength = 8
	passwordVaultBytes           = 8 << 20
	passwordVaultMaxItems        = 5000
	passwordVaultMaxGroups       = 200
	passwordVaultNoGroup         = 0
	passwordVaultAllGroups       = -1
	vaultLogin                   = "login"
	vaultKey                     = "key"
	vaultNote                    = "note"
)

var (
	errVaultUnlock        = errors.New("主密码错误，或密码库已损坏")
	errVaultChanged       = errors.New("密码库已在其他窗口或程序中更改，请锁定后重新解锁")
	passwordVaultFileLock sync.Mutex
)

type passwordGroup struct {
	ID         int
	Parent     int `json:",omitempty"`
	Name       string
	Production bool
}

type passwordEntry struct {
	ID, Group                               int
	Name, Kind, Site, Account, Secret, Note string
	Tags                                    []string
	Favorite                                bool
}

type passwordVaultData struct {
	Next                          int
	DefaultGroup                  int `json:",omitempty"`
	Groups                        []passwordGroup
	Entries                       []passwordEntry
	IdleMinutes, ClipboardSeconds int
	DisableIdleLock               bool `json:",omitempty"`
	DisableClipboardCleanup       bool `json:",omitempty"`
}

type unlockedPasswordVault struct {
	data     passwordVaultData
	key      []byte
	header   []byte
	revision [32]byte
}

func newPasswordVaultData() passwordVaultData {
	return passwordVaultData{
		Next: 1, DefaultGroup: 1, IdleMinutes: 5, ClipboardSeconds: 20,
		Groups: []passwordGroup{{ID: 1, Name: "默认分组"}},
	}
}

func (data passwordVaultData) clone() passwordVaultData {
	data.Groups = slices.Clone(data.Groups)
	data.Entries = slices.Clone(data.Entries)
	return data
}

func (data passwordVaultData) group(id int) passwordGroup {
	if id == passwordVaultNoGroup {
		return passwordGroup{Name: "未分组"}
	}
	for _, group := range data.Groups {
		if group.ID == id {
			return group
		}
	}
	return passwordGroup{}
}

func (data passwordVaultData) groupsByParent() map[int][]passwordGroup {
	children := make(map[int][]passwordGroup)
	for _, group := range data.Groups {
		children[group.Parent] = append(children[group.Parent], group)
	}
	return children
}

func (data passwordVaultData) groupRemoval(id int) (map[int]bool, int) {
	children := data.groupsByParent()
	removed := make(map[int]bool)
	var mark func(int)
	mark = func(id int) {
		removed[id] = true
		for _, child := range children[id] {
			mark(child.ID)
		}
	}
	mark(id)
	if parent := data.group(id).Parent; parent != 0 {
		return removed, parent
	}
	if !removed[data.DefaultGroup] {
		return removed, data.DefaultGroup
	}
	for _, group := range children[0] {
		if !removed[group.ID] {
			return removed, group.ID
		}
	}
	return removed, passwordVaultNoGroup
}

func (data passwordVaultData) groupPath(id int) string {
	group := data.group(id)
	path := group.displayName()
	for group.Parent > 0 {
		group = data.group(group.Parent)
		path = group.displayName() + " / " + path
	}
	return path
}

func (group passwordGroup) displayName() string {
	if group.ID == 0 && group.Name == "未分组" || group.ID == 1 && group.Name == "默认分组" {
		return uiText(group.Name)
	}
	return group.Name
}

func (data passwordVaultData) entry(id int) (passwordEntry, bool) {
	for _, entry := range data.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return passwordEntry{}, false
}

func validatePasswordEntry(entry passwordEntry) error {
	if name := strings.TrimSpace(entry.Name); name == "" || utf8.RuneCountInString(name) > 80 {
		return errors.New(uiText("请输入 1 至 80 个字符的条目名称"))
	}
	switch entry.Kind {
	case vaultLogin:
		if strings.TrimSpace(entry.Account) == "" || entry.Secret == "" {
			return errors.New(uiText("请填写用户名和密码"))
		}
	case vaultKey:
		if entry.Secret == "" {
			return errors.New(uiText("请填写密钥"))
		}
	case vaultNote:
		if strings.TrimSpace(entry.Note) == "" {
			return errors.New(uiText("请填写安全备注"))
		}
	default:
		return errors.New(uiText("条目类型无效"))
	}
	if len(entry.Site) > 2048 || len(entry.Account) > 512 || len(entry.Secret) > 4096 || len(entry.Note) > 16<<10 || len(entry.Tags) > 20 {
		return errors.New(uiText("条目字段超出长度限制，最多可添加 20 个标签"))
	}
	for _, value := range []string{entry.Name, entry.Site, entry.Account, entry.Secret, entry.Note} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return errors.New(uiText("条目含有无效字符"))
		}
	}
	for _, tag := range entry.Tags {
		if !utf8.ValidString(tag) || strings.ContainsRune(tag, 0) || utf8.RuneCountInString(tag) > 32 {
			return errors.New(uiText("标签含有无效字符或超过 32 个字符"))
		}
	}
	return nil
}

func validPasswordVaultDuration(value int, unit time.Duration) bool {
	const maxDuration = time.Duration(1<<63 - 1)
	return value > 0 && int64(value) <= int64(maxDuration/unit)
}

func validatePasswordVault(data passwordVaultData) error {
	if data.Next < 0 || data.Next > 1<<30 || len(data.Entries) > passwordVaultMaxItems || len(data.Groups) > passwordVaultMaxGroups {
		return errors.New(uiText("密码库超出条目或分组数量限制"))
	}
	if !validPasswordVaultDuration(data.IdleMinutes, time.Minute) || !validPasswordVaultDuration(data.ClipboardSeconds, time.Second) {
		return errors.New(uiText("密码库的锁定或剪贴板设置无效"))
	}
	ids := make(map[int]bool, len(data.Groups)+len(data.Entries))
	groups := make(map[int]passwordGroup, len(data.Groups))
	type groupName struct {
		parent int
		name   string
	}
	names := map[groupName]bool{{0, "全部条目"}: true, {0, "未分组"}: true}
	for _, group := range data.Groups {
		name := strings.ToLower(strings.TrimSpace(group.Name))
		key := groupName{group.Parent, name}
		if group.ID <= 0 || group.ID > data.Next || ids[group.ID] || name == "" || utf8.RuneCountInString(name) > 32 ||
			!utf8.ValidString(name) || strings.ContainsRune(name, 0) || names[key] {
			return errors.New(uiText("分组名称须为 1 至 32 个字符，且在同一级内唯一"))
		}
		ids[group.ID], groups[group.ID], names[key] = true, group, true
	}
	if data.DefaultGroup != 0 {
		if group, exists := groups[data.DefaultGroup]; !exists || group.Parent != 0 {
			return errors.New(uiText("默认分组无效"))
		}
	}
	for _, group := range data.Groups {
		for parent, depth := group.Parent, 0; parent != 0; depth++ {
			ancestor, exists := groups[parent]
			if !exists || depth >= len(groups)-1 {
				return errors.New(uiText("分组层级无效"))
			}
			parent = ancestor.Parent
		}
	}
	for _, entry := range data.Entries {
		_, inGroup := groups[entry.Group]
		if entry.ID <= 0 || entry.ID > data.Next || ids[entry.ID] || entry.Group != passwordVaultNoGroup && !inGroup {
			return errors.New(uiText("密码库中有无效的条目标识或分组"))
		}
		if err := validatePasswordEntry(entry); err != nil {
			return err
		}
		ids[entry.ID] = true
	}
	return nil
}

func passwordVaultPath(directory string) string { return filepath.Join(directory, passwordVaultName) }

func passwordVaultCipher(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}

func passwordVaultUsesWindows(encoded []byte) bool {
	return bytes.HasPrefix(encoded, []byte(passwordVaultWindowsMagic))
}

func passwordVaultHeaderSize(encoded []byte) int {
	if len(encoded) > passwordVaultBytes {
		return 0
	}
	if passwordVaultUsesWindows(encoded) {
		prefix := len(passwordVaultWindowsMagic) + 2
		if len(encoded) < prefix {
			return 0
		}
		size := int(binary.LittleEndian.Uint16(encoded[len(passwordVaultWindowsMagic):prefix]))
		if size > 0 && size <= passwordVaultMaxWrappedKey && len(encoded) >= prefix+size+28 {
			return prefix + size
		}
		return 0
	}
	size := len(passwordVaultMagic) + passwordVaultSaltSize
	if len(encoded) >= size+28 && bytes.HasPrefix(encoded, []byte(passwordVaultMagic)) {
		return size
	}
	return 0
}

func unlockPasswordVault(encoded []byte, password string) (*unlockedPasswordVault, error) {
	headerSize := passwordVaultHeaderSize(encoded)
	if headerSize == 0 {
		return nil, errors.New(uiText("密码库格式无效或版本不受支持"))
	}
	header := bytes.Clone(encoded[:headerSize])
	var key []byte
	var err error
	if passwordVaultUsesWindows(header) {
		key, err = windowsUserData(header[len(passwordVaultWindowsMagic)+2:], false)
		if err != nil || len(key) != 32 {
			clear(key)
			return nil, errors.New(uiText("当前 Windows 账户无法解锁密码本"))
		}
	} else {
		if len(password) == 0 || len(password) > 4096 {
			return nil, errors.New(uiText("请输入有效的主密码"))
		}
		key, err = pbkdf2.Key(sha256.New, password, header[len(passwordVaultMagic):], passwordVaultRounds, 32)
		if err != nil {
			return nil, err
		}
	}
	vault := &unlockedPasswordVault{key: key, header: header, revision: sha256.Sum256(encoded)}
	block, err := passwordVaultCipher(key)
	if err != nil {
		vault.clear()
		return nil, err
	}
	plain, err := block.Open(nil, nil, encoded[headerSize:], header)
	if err != nil {
		vault.clear()
		return nil, errVaultUnlock
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, &vault.data); err != nil || validatePasswordVault(vault.data) != nil {
		vault.clear()
		return nil, errors.New(uiText("无法读取密码库中的条目"))
	}
	return vault, nil
}

func createPasswordVault(password string) (*unlockedPasswordVault, error) {
	if utf8.RuneCountInString(password) < passwordVaultMinMasterLength || len(password) > 4096 || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf(uiText("密码至少需要 %d 个字符，最多 4096 字节"), passwordVaultMinMasterLength)
	}
	salt := make([]byte, passwordVaultSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordVaultRounds, 32)
	if err != nil {
		return nil, err
	}
	return &unlockedPasswordVault{data: newPasswordVaultData(), key: key, header: append([]byte(passwordVaultMagic), salt...)}, nil
}

func createWindowsPasswordVault() (*unlockedPasswordVault, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		clear(key)
		return nil, err
	}
	wrapped, err := windowsUserData(key, true)
	if err != nil || len(wrapped) == 0 || len(wrapped) > passwordVaultMaxWrappedKey {
		clear(key)
		return nil, errors.New(uiText("当前 Windows 账户无法保护密码本"))
	}
	header := binary.LittleEndian.AppendUint16([]byte(passwordVaultWindowsMagic), uint16(len(wrapped)))
	return &unlockedPasswordVault{data: newPasswordVaultData(), key: key, header: append(header, wrapped...)}, nil
}

func (vault *unlockedPasswordVault) clear() {
	clear(vault.key)
	clear(vault.data.Entries)
	*vault = unlockedPasswordVault{}
}

// The window's decrypted state is updated only after the authenticated file is replaced.
func (vault *unlockedPasswordVault) save(path string, data passwordVaultData) error {
	return vault.saveWithArchive(path, data, false)
}

func (vault *unlockedPasswordVault) saveWithArchive(path string, data passwordVaultData, archiveCurrent bool) error {
	encoded, err := vault.encode(data)
	if err != nil {
		return err
	}
	passwordVaultFileLock.Lock()
	defer passwordVaultFileLock.Unlock()
	current, err := readTextAsset(path, passwordVaultBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if (err == nil && sha256.Sum256(current) != vault.revision) || (err != nil && vault.revision != [32]byte{}) {
		return errVaultChanged
	}
	if archiveCurrent && len(current) != 0 {
		if err := writeTextAsset(newPasswordArchivePath(path), current, passwordVaultBytes); err != nil {
			return fmt.Errorf(uiText("无法归档当前密码本：%w"), err)
		}
	}
	if err := writeTextAsset(path, encoded, passwordVaultBytes); err != nil {
		return fmt.Errorf(uiText("无法保存加密密码库：%w"), err)
	}
	vault.data, vault.revision = data, sha256.Sum256(encoded)
	return nil
}

func (vault *unlockedPasswordVault) encode(data passwordVaultData) ([]byte, error) {
	if err := validatePasswordVault(data); err != nil {
		return nil, err
	}
	plain, err := json.Marshal(data)
	if err != nil {
		return nil, errors.New(uiText("无法编码密码库"))
	}
	defer clear(plain)
	if len(plain)+len(vault.header)+28 > passwordVaultBytes {
		return nil, errors.New(uiText("密码库超过 8 MiB"))
	}
	block, err := passwordVaultCipher(vault.key)
	if err != nil {
		return nil, err
	}
	return block.Seal(bytes.Clone(vault.header), nil, plain, vault.header), nil
}

// A backup can initialize a new vault only after its password and authenticated contents are verified.
func restorePasswordVault(path, backup string, password string) (*unlockedPasswordVault, error) {
	encoded, err := readTextAsset(backup, passwordVaultBytes)
	if err != nil {
		return nil, err
	}
	vault, err := unlockPasswordVault(encoded, password)
	if err != nil {
		return nil, err
	}
	vault.revision = [32]byte{}
	if err := vault.save(path, vault.data); err != nil {
		vault.clear()
		return nil, err
	}
	return vault, nil
}

func passwordSite(entry passwordEntry) (key, label string) {
	if entry.Kind != vaultLogin || entry.Site == "" {
		return "", ""
	}
	site, err := url.Parse(normalizePasswordSite(entry.Site))
	if err != nil || site == nil || site.Host == "" {
		return "", ""
	}
	port := site.Port()
	if site.Scheme == "https" && port == "443" || site.Scheme == "http" && port == "80" {
		port = ""
	}
	host := strings.ToLower(strings.TrimSuffix(site.Hostname(), "."))
	return fmt.Sprintf("%d|%s|%s|%s", entry.Group, site.Scheme, host, port), site.Host
}
