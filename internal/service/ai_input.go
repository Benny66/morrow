package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"morrow/internal/ai"
	"morrow/internal/apperr"
)

// 附件读取的上限与类型表。
//
// 【为什么这些上限放在 service 而不在 ai】它们是"用户能塞多大的输入"这类
// 产品约束，与协议无关；ai 包只负责把已经合法的材料编成请求体。
const (
	// maxTextAttachmentBytes 文本附件的读取上限（200KB）。超出部分截断——
	// 截断比拒绝更符合直觉：用户选中一份日志，通常只想让模型看开头。
	maxTextAttachmentBytes = 200 << 10
	// maxImageAttachmentBytes 单张图片的上限（5MB，base64 后约 6.7MB）。
	// 图片不能截断（截一半就是坏图），故超出直接报错。
	maxImageAttachmentBytes = 5 << 20
	// maxAttachmentCount 单次请求的附件数量上限。一道护栏：多张高分辨率图片
	// 很快就撞上端点的请求体上限，而那时的报错（413/400）指不到具体哪一张。
	maxAttachmentCount = 8
	// truncatedNote 追加在超长文本附件末尾的说明。
	truncatedNote = "\n（内容过长已截断）"
)

// textAttachmentExts 是当纯文本读取的扩展名。
//
// 只列"人能直接读懂"的纯文本格式：这些文件里除了文字没有别的语义，
// 直接塞进 prompt 不会失真。
var textAttachmentExts = map[string]bool{
	".txt":      true,
	".md":       true,
	".markdown": true,
	".csv":      true,
	".json":     true,
	".log":      true,
}

// imageAttachmentExts 是支持的多模态图片扩展名 → MIME。
//
// 只收 OpenAI 兼容端点普遍支持的四种：多收一些（如 .heic / .tiff）看着更宽容，
// 但端点不支持时用户拿到的是"生成失败"，反而更难排查。
var imageAttachmentExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// InlineImage 是粘贴/拖拽进来的图片（不落盘，经 IPC 以 base64 传入）。
//
// 【为什么是 base64 字符串而不是 []byte】wails 绑定把 []byte 序列化成什么形态
// 取决于生成器版本；base64 字符串在 JSON 里是确定的一种形状，5MB 图片也只有
// ~6.7MB，不会像 number[] 那样膨胀成几十 MB 的 JSON。
type InlineImage struct {
	// Name 文件名（仅用于展示/提示）。
	Name string `json:"name"`
	// MIME 图片 MIME（image/png 等）。
	MIME string `json:"mime"`
	// DataBase64 图片原始字节的 base64（可带 data URI 前缀）。
	DataBase64 string `json:"data_base64"`
}

// GeneratePlanFromInput 用「自然语言描述 + 附件」生成一份提醒计划【草稿】（不落库）。
//
// 【为什么不落库】用户可能只是想看看模型怎么理解这段描述；直接写库会替换掉现有
// 计划（见 GeneratePlan 的替换语义），一次试错就毁掉一份能用的计划。
// 草稿交给前端展示，用户确认后再逐条 CreateReminder 落库——复用的仍是那条
// 带完整校验的写入路径，不存在"AI 生成的东西绕过校验"的旁路。
func (s *Service) GeneratePlanFromInput(settings ai.Settings, description string, filePaths []string) (*ai.Plan, error) {
	attachments, err := LoadAttachments(filePaths)
	if err != nil {
		return nil, err
	}

	client := ai.NewClient(settings)
	ctx, cancel := context.WithTimeout(context.Background(), ai.RequestTimeout)
	// 取消必须发生在请求结束之后：defer 保证异常路径也会释放 context。
	defer cancel()

	plan, generateErr := client.GeneratePlan(ctx, description, attachments)
	if generateErr != nil {
		return nil, apperr.Wrap(generateErr)
	}
	return &plan, nil
}

// LoadAttachments 读取用户选中的文件（兼容旧调用点）。
func LoadAttachments(paths []string) ([]ai.Attachment, error) {
	return LoadAttachmentsWithInline(paths, nil)
}

// LoadAttachmentsWithInline 合并「文件路径」与「粘贴/拖拽图片」两种来源。
//
// 空路径会被跳过、重复路径与重复内联图片会被去重：前端是"一次选一个、可重复
// 添加"的交互，同一份材料被加两次既费 token，又会让模型把同一件事写两遍。
// 总条数上限同时约束两类来源；图片大小/格式在读取时校验。
func LoadAttachmentsWithInline(paths []string, inline []InlineImage) ([]ai.Attachment, error) {
	if len(paths)+len(inline) > maxAttachmentCount {
		return nil, apperr.Validation(
			fmt.Sprintf("一次最多添加 %d 个附件", maxAttachmentCount))
	}
	out := make([]ai.Attachment, 0, len(paths)+len(inline))
	seen := make(map[string]bool, len(paths)+len(inline))
	for _, path := range paths {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		attachment, err := loadAttachment(trimmed)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
	}
	for _, image := range inline {
		attachment, err := loadInlineImage(image)
		if err != nil {
			return nil, err
		}
		// 用内容哈希去重：同名同长度的两张不同图片不能被误判成重复。
		sum := sha256.Sum256(attachment.Data)
		key := fmt.Sprintf("inline:%x", sum)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, attachment)
	}
	return out, nil
}

// loadInlineImage 校验并解码一张粘贴/拖拽图片。
func loadInlineImage(image InlineImage) (ai.Attachment, error) {
	name := strings.TrimSpace(image.Name)
	if name == "" {
		name = "粘贴的图片"
	}
	mime := strings.ToLower(strings.TrimSpace(image.MIME))
	if mime == "" {
		mime = inlineImageMIME(name)
	}
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	if !supportedImageMIME(mime) {
		return ai.Attachment{}, apperr.Validation("不支持的图片格式，仅支持 png / jpg / webp / gif")
	}
	payload := stripDataURIPrefix(image.DataBase64)
	// 先按 base64 长度估算解码后大小，避免为一个超大字符串先分配几十 MB 再拒绝。
	if base64.StdEncoding.DecodedLen(len(payload)) > maxImageAttachmentBytes {
		return ai.Attachment{}, apperr.Validation(
			fmt.Sprintf("图片 %s 超过 5MB，请压缩后再试", name))
	}
	data, err := decodeInlineBase64(payload)
	if err != nil {
		return ai.Attachment{}, apperr.Validation("图片数据无法解码，请重新粘贴")
	}
	if len(data) == 0 {
		return ai.Attachment{}, apperr.Validation("图片内容为空，请重新粘贴")
	}
	if len(data) > maxImageAttachmentBytes {
		return ai.Attachment{}, apperr.Validation(
			fmt.Sprintf("图片 %s 超过 5MB，请压缩后再试", name))
	}
	return ai.Attachment{Name: name, Kind: ai.AttachmentImage, MIME: mime, Data: data}, nil
}

// stripDataURIPrefix 去掉 `data:image/png;base64,` 前缀；没有前缀时原样返回。
func stripDataURIPrefix(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "data:") {
		if index := strings.Index(text, ","); index >= 0 {
			return text[index+1:]
		}
	}
	return text
}

// decodeInlineBase64 解码 base64；容忍 data URI 前缀与缺失的 padding。
func decodeInlineBase64(text string) ([]byte, error) {
	text = stripDataURIPrefix(text)
	data, err := base64.StdEncoding.DecodeString(text)
	if err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(strings.TrimRight(text, "="))
}

// supportedImageMIME 判断 MIME 是否在支持的图片格式表里。
func supportedImageMIME(mime string) bool {
	for _, supported := range imageAttachmentExts {
		if supported == mime {
			return true
		}
	}
	return false
}

// inlineImageMIME 在 MIME 缺失时按文件名后缀推断。
func inlineImageMIME(name string) string {
	return imageAttachmentExts[strings.ToLower(filepath.Ext(name))]
}

// loadAttachment 读取单个文件并按扩展名分流。
func loadAttachment(path string) (ai.Attachment, error) {
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))

	if mime, ok := imageAttachmentExts[ext]; ok {
		return loadImageAttachment(path, name, mime)
	}
	if textAttachmentExts[ext] {
		return loadTextAttachment(path, name)
	}

	// 【为什么不支持 PDF】本仓零新增依赖，而 PDF 文本提取必须引入解析库；
	// 且各家 PDF 的排版（多栏、扫描件、图文混排）差异极大，提取质量参差。
	// 与其把抽出来的碎片喂给模型、让它编出一套离谱的计划，不如明确告知用户
	// 复制其中的文字重试——这是一次"说清限制"，不是一次静默降级。
	label := ext
	if label == "" {
		label = "该"
	}
	return ai.Attachment{}, apperr.Validation(
		fmt.Sprintf("暂不支持 %s 格式，请复制其中的文字后再试", label))
}

// loadTextAttachment 把文本文件读进内存（超长时截断）。
func loadTextAttachment(path, name string) (ai.Attachment, error) {
	file, err := os.Open(path)
	if err != nil {
		return ai.Attachment{}, apperr.Wrap(fmt.Errorf("打开文件失败: %w", err))
	}
	// 关闭只读文件：这里的错误没有补救动作（数据已读完），显式忽略并说明原因。
	defer func() { _ = file.Close() }()

	// 多读一个字节即可判断"是否还有更多内容"，省掉一次 Stat。
	buf, readErr := io.ReadAll(io.LimitReader(file, maxTextAttachmentBytes+1))
	if readErr != nil {
		return ai.Attachment{}, apperr.Wrap(fmt.Errorf("读取文件失败: %w", readErr))
	}

	text := string(buf)
	if len(buf) > maxTextAttachmentBytes {
		// 按字节截断可能切在多字节字符中间：ToValidUTF8 把残字节换成替换符，
		// 比把半个汉字发给模型好。
		text = strings.ToValidUTF8(text[:maxTextAttachmentBytes], "") + truncatedNote
	}
	return ai.Attachment{Name: name, Kind: ai.AttachmentText, Text: text}, nil
}

// loadImageAttachment 把图片文件读成原始字节（附带 MIME）。
func loadImageAttachment(path, name, mime string) (ai.Attachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return ai.Attachment{}, apperr.Wrap(fmt.Errorf("读取文件信息失败: %w", err))
	}
	if info.Size() > maxImageAttachmentBytes {
		return ai.Attachment{}, apperr.Validation(
			fmt.Sprintf("图片 %s 超过 5MB，请压缩后再试", name))
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return ai.Attachment{}, apperr.Wrap(fmt.Errorf("读取图片失败: %w", readErr))
	}
	return ai.Attachment{Name: name, Kind: ai.AttachmentImage, MIME: mime, Data: data}, nil
}
