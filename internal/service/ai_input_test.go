package service

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"morrow/internal/ai"
)

func TestLoadInlineImage(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a}
	list, err := LoadAttachmentsWithInline(nil, []InlineImage{{
		Name:       "paste.png",
		MIME:       "image/png",
		DataBase64: base64.StdEncoding.EncodeToString(data),
	}})
	if err != nil {
		t.Fatalf("加载内联图片失败: %v", err)
	}
	if len(list) != 1 || list[0].Kind != ai.AttachmentImage || list[0].MIME != "image/png" {
		t.Fatalf("附件不符: %+v", list)
	}
	if string(list[0].Data) != string(data) {
		t.Errorf("图片字节不一致")
	}
}

func TestLoadInlineImageDataURIAndNameInference(t *testing.T) {
	data := []byte("fake-png")
	raw := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	list, err := LoadAttachmentsWithInline(nil, []InlineImage{{
		Name:       "截图.png",
		DataBase64: raw,
	}})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(list) != 1 || list[0].MIME != "image/png" {
		t.Fatalf("MIME 推断失败: %+v", list)
	}
}

func TestLoadInlineImageRejectsUnsupportedMIME(t *testing.T) {
	_, err := LoadAttachmentsWithInline(nil, []InlineImage{{
		Name:       "x.heic",
		MIME:       "image/heic",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("x")),
	}})
	if err == nil {
		t.Fatal("不支持的图片格式应被拒绝")
	}
}

func TestLoadInlineImageRejectsOversize(t *testing.T) {
	data := make([]byte, maxImageAttachmentBytes+1)
	_, err := LoadAttachmentsWithInline(nil, []InlineImage{{
		Name:       "big.png",
		MIME:       "image/png",
		DataBase64: base64.StdEncoding.EncodeToString(data),
	}})
	if err == nil {
		t.Fatal("超过 5MB 的图片应被拒绝")
	}
}

func TestLoadAttachmentsCombinedLimit(t *testing.T) {
	inline := make([]InlineImage, 0, maxAttachmentCount)
	for i := 0; i < maxAttachmentCount; i++ {
		inline = append(inline, InlineImage{
			Name:       "x.png",
			MIME:       "image/png",
			DataBase64: base64.StdEncoding.EncodeToString([]byte{byte(i)}),
		})
	}
	if _, err := LoadAttachmentsWithInline([]string{"not-read.txt"}, inline); err == nil {
		t.Fatal("文件 + 内联图片总数超过 8 应被拒绝")
	}
}

func TestLoadAttachmentsDeduplicatesInlineAndPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}
	inline := InlineImage{
		Name:       "same.png",
		MIME:       "image/png",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("same")),
	}
	list, err := LoadAttachmentsWithInline([]string{path, path}, []InlineImage{inline, inline})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("去重后条数 = %d，期望 2（文本 + 图片）", len(list))
	}
}

// TestLoadInlineImagesSameNameDifferentContentKept 断言去重按内容而不是名字+长度：
// 两张同名同长度的不同截图不能被误判成重复而静默丢掉一张。
func TestLoadInlineImagesSameNameDifferentContentKept(t *testing.T) {
	list, err := LoadAttachmentsWithInline(nil, []InlineImage{
		{Name: "same.png", MIME: "image/png", DataBase64: base64.StdEncoding.EncodeToString([]byte("aaaa"))},
		{Name: "same.png", MIME: "image/png", DataBase64: base64.StdEncoding.EncodeToString([]byte("bbbb"))},
	})
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("同名不同内容的图片被误去重，实际 %d 条", len(list))
	}
}
