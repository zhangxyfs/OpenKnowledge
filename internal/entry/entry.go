package entry

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Entry struct {
	Title     string   `yaml:"title"`
	Type      string   `yaml:"type"`
	Tags      []string `yaml:"tags"`
	Mandatory bool     `yaml:"mandatory"`
	Summary   string   `yaml:"summary"`
	Body      string   `yaml:"-"`
	Path      string   `yaml:"-"`
}

var validTypes = map[string]bool{"rule": true, "pitfall": true, "note": true, "reference": true}

func ValidType(t string) bool { return validTypes[t] }

// Parse 解析 "---\n<yaml>\n---\n<body>" 格式的条目文件；容忍 CRLF 与 UTF-8 BOM。
func Parse(content []byte) (*Entry, error) {
	s := strings.TrimPrefix(string(content), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, "---\n") {
		return nil, fmt.Errorf("缺少 frontmatter 起始 ---")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	delim := "\n---\n"
	if end < 0 && strings.HasSuffix(rest, "\n---") {
		end = len(rest) - len("\n---")
		delim = "\n---"
	}
	if end < 0 {
		return nil, fmt.Errorf("缺少 frontmatter 结束 ---")
	}
	e := &Entry{}
	if err := yaml.Unmarshal([]byte(rest[:end]), e); err != nil {
		return nil, fmt.Errorf("解析 frontmatter: %w", err)
	}
	if e.Title == "" {
		return nil, fmt.Errorf("缺少 title")
	}
	if !ValidType(e.Type) {
		return nil, fmt.Errorf("非法 type %q（rule|pitfall|note|reference）", e.Type)
	}
	e.Body = strings.TrimSpace(rest[end+len(delim):])
	return e, nil
}

func (e *Entry) Serialize() []byte {
	fm, err := yaml.Marshal(e)
	if err != nil {
		panic(err)
	}
	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fm)
	buf.WriteString("---\n\n")
	buf.WriteString(e.Body)
	buf.WriteString("\n")
	return buf.Bytes()
}

// Load 读取目录下全部 .md 条目，按文件名排序。
func Load(dir string) ([]*Entry, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	var entries []*Entry
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		e, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", m, err)
		}
		e.Path = m
		entries = append(entries, e)
	}
	return entries, nil
}

// Slug 将标题转为安全文件名（不含扩展名）。
func Slug(title string) string {
	title = strings.TrimSpace(title)
	title = strings.ReplaceAll(title, " ", "-")
	return strings.Map(func(r rune) rune {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			return -1
		}
		return r
	}, title)
}

// FileName 返回条目在磁盘上的文件名。
func (e *Entry) FileName() string {
	if e.Path != "" {
		return filepath.Base(e.Path)
	}
	return Slug(e.Title) + ".md"
}

// EmbedText 是计算 embedding 时使用的文本。
func (e *Entry) EmbedText() string {
	return e.Title + "\n" + e.Summary + "\n" + e.Body
}
