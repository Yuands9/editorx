package editor

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const passwordImportLimit = 64 << 10
const passwordImportRows = 120

var (
	passwordNumberedLine = regexp.MustCompile("^[0-9]{1,3}\\s*(?:[、)）]\\s*|\\.(?:\\s+|$))")
	passwordLabel        = regexp.MustCompile("(?i)(^|[\\s　,，;；])(?:\\*\\*)?(账号|帐号|账户|用户名|用户|username|account|user|密码|口令|password|pwd|pass)(?:\\*\\*)?\\s*[:：]\\s*")
	passwordURLLabel     = regexp.MustCompile("(?i)^(?:网站|网址|链接|地址|登录地址|url)\\s*[:：]\\s*")
	passwordMarkdownLink = regexp.MustCompile("(?i)^\\[[^\\]]*\\]\\((https?://[^[:space:]]+)\\)$")
	passwordTableDivider = regexp.MustCompile("^:?-{3,}:?$")
)

func normalizePasswordSite(value string) string {
	value = passwordURLLabel.ReplaceAllString(strings.TrimSpace(value), "")
	if link := passwordMarkdownLink.FindStringSubmatch(value); link != nil {
		value = link[1]
	}
	value = strings.TrimPrefix(strings.TrimSuffix(value, ">"), "<")
	if value == "" || strings.IndexFunc(value, unicode.IsSpace) >= 0 {
		return ""
	}
	explicit := strings.HasPrefix(strings.ToLower(value), "https://") || strings.HasPrefix(strings.ToLower(value), "http://")
	if !explicit {
		value = "https://" + value
	}
	site, err := url.Parse(value)
	if err != nil || site.Hostname() == "" || site.User != nil || (!explicit && !strings.ContainsAny(site.Hostname(), ".:")) {
		return ""
	}
	if strings.ContainsAny(site.Hostname(), "\\|{}[]<>\"`;,=") {
		return ""
	}
	return value
}

func passwordImportField(value string) string {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(value), "*_` ")) {
	case "名称", "应用", "服务", "系统", "标题", "name", "title":
		return "name"
	case "网址", "网站", "登录地址", "地址", "链接", "url", "site":
		return "site"
	case "账号", "帐号", "账户", "用户名", "用户", "username", "account", "user":
		return "account"
	case "密码", "口令", "password", "pwd", "pass":
		return "secret"
	}
	return ""
}

func passwordImportCell(value string) string {
	value = strings.TrimSpace(value)
	if leading := len(value) - len(strings.TrimLeft(value, "`")); leading > 0 && len(value) > leading*2 && strings.HasSuffix(value, strings.Repeat("`", leading)) {
		return value[leading : len(value)-leading]
	}
	return value
}

func passwordTableCells(line string) []string {
	if !strings.Contains(line, "|") {
		return nil
	}
	line = strings.TrimPrefix(strings.TrimSuffix(line, "|"), "|")
	var cells []string
	var cell strings.Builder
	for index := 0; index < len(line); index++ {
		switch {
		case line[index] == '\\' && index+1 < len(line) && line[index+1] == '|':
			cell.WriteByte('|')
			index++
		case line[index] == '|':
			cells = append(cells, passwordImportCell(cell.String()))
			cell.Reset()
		default:
			cell.WriteByte(line[index])
		}
	}
	return append(cells, passwordImportCell(cell.String()))
}

type passwordImportCandidate struct {
	entry  passwordEntry
	source string
}

// Parses only explicitly supplied text. Rows with missing fields remain drafts.
func detectPasswordEntries(input string) []passwordImportCandidate {
	if len(input) > passwordImportLimit || !utf8.ValidString(input) || strings.ContainsRune(input, 0) {
		return nil
	}
	var results []passwordImportCandidate
	var plain passwordEntry
	var header []string
	add := func(entry passwordEntry, source string) {
		if (entry.Account == "" && entry.Secret == "") || len(results) > passwordImportRows {
			return
		}
		entry.Kind = vaultLogin
		entry.Site = normalizePasswordSite(entry.Site)
		if entry.Name == "" {
			entry.Name = "未命名条目"
			if site, err := url.Parse(entry.Site); err == nil && site.Hostname() != "" {
				entry.Name = site.Hostname()
			}
		}
		results = append(results, passwordImportCandidate{entry, source})
	}
	flush := func() { add(plain, "纯文本"); plain = passwordEntry{} }
	for _, original := range strings.Split(input, "\n") {
		line := strings.TrimSpace(original)
		if number := passwordNumberedLine.FindStringIndex(line); number != nil {
			flush()
			header = nil
			line = line[number[1]:]
		}
		if line == "" || strings.HasPrefix(line, "```") {
			continue
		}
		cells := passwordTableCells(line)
		if len(cells) >= 2 {
			divider := true
			mapped := make([]string, len(cells))
			for index, cell := range cells {
				divider = divider && passwordTableDivider.MatchString(cell)
				mapped[index] = passwordImportField(cell)
			}
			if divider {
				continue
			}
			if slices.Contains(mapped, "account") && slices.Contains(mapped, "secret") {
				flush()
				header = mapped
				continue
			}
			if len(header) == len(cells) {
				entry := passwordEntry{}
				for index, field := range header {
					setPasswordImportValue(&entry, field, cells[index])
				}
				add(entry, "Markdown 表格")
				continue
			}
			if len(cells) == 4 && normalizePasswordSite(cells[1]) != "" {
				flush()
				add(passwordEntry{Name: cells[0], Site: cells[1], Account: cells[2], Secret: cells[3]}, "Markdown 表格")
				continue
			}
		}
		header = nil
		if site := normalizePasswordSite(line); site != "" {
			if plain.Site != "" {
				flush()
			}
			plain.Site = site
			continue
		}
		if labels := passwordLabel.FindAllStringSubmatchIndex(line, -1); len(labels) > 0 {
			for index, label := range labels {
				field := passwordImportField(line[label[4]:label[5]])
				if field == "account" && plain.Account != "" || field == "secret" && plain.Secret != "" {
					flush()
				}
				end := len(line)
				if index+1 < len(labels) {
					end = labels[index+1][0]
				}
				setPasswordImportValue(&plain, field, passwordImportCell(line[label[1]:end]))
			}
			continue
		}
		if utf8.RuneCountInString(line) <= 80 && !strings.ContainsAny(line, ":：|") {
			if plain.Account != "" || plain.Secret != "" {
				flush()
			}
			if plain.Name == "" {
				plain.Name = strings.TrimLeft(line, "#*- ")
			}
		}
	}
	flush()
	return results
}

func setPasswordImportValue(entry *passwordEntry, field, value string) {
	switch field {
	case "name":
		entry.Name = value
	case "site":
		entry.Site = value
	case "account":
		entry.Account = value
	case "secret":
		entry.Secret = value
	}
}
