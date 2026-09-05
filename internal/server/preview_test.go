package server

import (
	"projectboard/internal/domain"
	"strings"
	"testing"
)

func TestAttachmentPreviewFormatsAndSafety(t *testing.T) {
	a := domain.Attachment{Name: "readme.md", Type: "text/markdown"}
	v, e := PreviewAttachment(a, []byte("# 标题\n\n**重点**\n\n| 列 | 值 |\n|---|---|\n| A | B |\n\n<script>alert(1)</script>\n\n[危险](javascript:alert(1))"))
	if e != nil {
		t.Fatal(e)
	}
	html := v["html"].(string)
	if !strings.Contains(html, "<h1>标题</h1>") || !strings.Contains(html, "<strong>重点</strong>") || !strings.Contains(html, "<table>") || strings.Contains(html, "<script>") || strings.Contains(html, `href="javascript:`) {
		t.Fatal(html)
	}
	v, e = PreviewAttachment(domain.Attachment{Name: "plain.txt", Type: "text/plain"}, []byte("<b>保持原文</b>"))
	if e != nil || v["kind"] != "text" || v["text"] != "<b>保持原文</b>" {
		t.Fatal(v, e)
	}
	v, e = PreviewAttachment(domain.Attachment{Name: "utf16.txt", Type: "text/plain"}, []byte{0xff, 0xfe, 'H', 0, 'i', 0})
	if e != nil || v["text"] != "Hi" {
		t.Fatal(v, e)
	}
	v, e = PreviewAttachment(domain.Attachment{Name: "photo.png", Type: "image/png"}, nil)
	if e != nil || v["kind"] != "image" {
		t.Fatal(v, e)
	}
	_, e = PreviewAttachment(domain.Attachment{Name: "archive.zip", Type: "application/zip"}, []byte{0, 1})
	if e == nil {
		t.Fatal("binary rendered as text")
	}
}
