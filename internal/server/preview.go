package server

import (
	"bytes"
	"fmt"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"golang.org/x/text/encoding/unicode"
	"path/filepath"
	"projectboard/internal/domain"
	"strings"
	"unicode/utf8"
)

func PreviewAttachment(a domain.Attachment, b []byte) (map[string]any, error) {
	if domain.In(a.Type, "image/png", "image/jpeg", "image/gif", "image/webp") {
		return map[string]any{"kind": "image", "name": a.Name}, nil
	}
	ext := strings.ToLower(filepath.Ext(a.Name))
	markdown := domain.In(ext, ".md", ".markdown") || a.Type == "text/markdown"
	if !markdown && !strings.HasPrefix(a.Type, "text/") && !domain.In(ext, ".txt", ".log", ".csv", ".json", ".yaml", ".yml", ".toml", ".ini", ".xml", ".sql", ".sh", ".ps1", ".go", ".js", ".ts", ".css", ".html") {
		return nil, fmt.Errorf("下载文件后，使用对应应用打开")
	}
	if bytes.HasPrefix(b, []byte{0xff, 0xfe}) || bytes.HasPrefix(b, []byte{0xfe, 0xff}) {
		decoded, e := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(b)
		if e != nil {
			return nil, fmt.Errorf("请使用 UTF-8 或 UTF-16 编码的文本")
		}
		b = decoded
	} else {
		b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	}
	if !utf8.Valid(b) || bytes.ContainsRune(b, 0) {
		return nil, fmt.Errorf("请使用 UTF-8 或 UTF-16 编码的文本")
	}
	truncated := len(b) > 1<<20
	if truncated {
		b = b[:1<<20]
		for !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
	}
	result := map[string]any{"kind": "text", "name": a.Name, "text": string(b), "truncated": truncated}
	if markdown {
		var out bytes.Buffer
		md := goldmark.New(goldmark.WithExtensions(extension.GFM))
		if e := md.Convert(b, &out); e != nil {
			return nil, e
		}
		result["kind"] = "markdown"
		result["html"] = out.String()
	}
	return result, nil
}
