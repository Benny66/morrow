package ai

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// 多模态内容的片段类型（OpenAI 兼容协议的 value）。
const (
	// partTypeText 文本片段。
	partTypeText = "text"
	// partTypeImageURL 图片片段（url 里放 data URI）。
	partTypeImageURL = "image_url"
)

// 附件类型（Attachment.Kind 的取值）。
const (
	// AttachmentText 文本附件：内容拼进 user 消息的正文。
	AttachmentText = "text"
	// AttachmentImage 图片附件：作为 content part 发给模型。
	AttachmentImage = "image"
)

// defaultAttachmentPrompt 是「只给了附件、没写字」时的兜底正文。
//
// 【为什么兜底而不是报错】用户发一张排班表照片，本身就是一次完整的表达；
// 逼他再写一句「帮我生成计划」只是形式主义。但 content 不能为空串
// （部分端点会直接 400），故给一句中性的引导语。
const defaultAttachmentPrompt = "请根据下面提供的材料生成提醒计划。"

// Attachment 是喂给 AI 的一份用户材料。
//
// 【为什么不在这里读文件】ai 包刻意保持无 IO（见包注释：它只做「发一次请求、
// 读一次响应」）。文件读取属于「用户的输入从哪来」，放在 service 层，
// 这样本包仍然可以只用假数据单测请求组装。
type Attachment struct {
	// Name 文件名，用于在 prompt 里标注来源。
	Name string
	// Kind 附件类型，取值见 AttachmentText / AttachmentImage。
	Kind string
	// Text Kind=text 时的正文。
	Text string
	// MIME Kind=image 时的 MIME（如 image/png）。
	MIME string
	// Data Kind=image 时的原始字节。
	Data []byte
}

// contentPart 是 content 数组里的一段。
type contentPart struct {
	// Type 片段类型，取值见 partType*。
	Type string `json:"type"`
	// Text Type=text 时的正文。
	Text string `json:"text,omitempty"`
	// ImageURL Type=image_url 时的图片地址。
	ImageURL *imageURL `json:"image_url,omitempty"`
}

// imageURL 是图片片段的地址载体。
//
// 本仓只发 data URI（本地文件转 base64），不代理任何远程图片：
// 让模型去抓一个 URL 会把「一次生成」变成「一次生成 + 一次服务端抓取」，
// 失败面（对方网络、防盗链）全在模型那边，用户却只看到一句「生成失败」。
type imageURL struct {
	// URL 图片地址，形如 data:image/png;base64,<...>。
	URL string `json:"url"`
}

// messageContent 是一条消息的内容：要么纯文本（string），要么多模态片段数组。
//
// 【为什么不直接把 Content 声明成 any】any 会让「一条消息到底带没带图」
// 退化成运行期断言，且任何一次误赋值都能编过。用这个只有两个字段的结构体，
// 构造入口就只有 textContent / partsContent 两个，形态在编译期就定死了。
//
// 两种形态在 JSON 里是不同类型（string vs array），故用 MarshalJSON 二选一。
type messageContent struct {
	// text 纯文本形态的正文。
	text string
	// parts 多模态形态的片段列表（非空时优先）。
	parts []contentPart
}

// textContent 构造纯文本内容。
func textContent(s string) messageContent {
	return messageContent{text: s}
}

// partsContent 构造多模态内容。
func partsContent(parts []contentPart) messageContent {
	return messageContent{parts: parts}
}

// MarshalJSON 按形态序列化：有片段用数组，否则用字符串。
//
// 【为什么要显式实现】messageContent 的字段都是非导出的，默认序列化会得到 "{}"；
// 而这里需要的恰恰是「往外是 string 或 array」两种完全不同的形状。
func (c messageContent) MarshalJSON() ([]byte, error) {
	if len(c.parts) > 0 {
		return json.Marshal(c.parts)
	}
	return json.Marshal(c.text)
}

// chatMessage 是一条对话消息。
type chatMessage struct {
	// Role system / user / assistant。
	Role string `json:"role"`
	// Content 消息内容（纯文本或多模态片段）。
	Content messageContent `json:"content"`
}

// textMessage 构造一条纯文本消息（绝大多数调用点用它）。
func textMessage(role, text string) chatMessage {
	return chatMessage{Role: role, Content: textContent(text)}
}

// partsMessage 构造一条多模态消息。
func partsMessage(role string, parts []contentPart) chatMessage {
	return chatMessage{Role: role, Content: partsContent(parts)}
}

// hasAttachment 判断附件列表里是否至少有一份可用材料。
func hasAttachment(attachments []Attachment) bool {
	for _, attachment := range attachments {
		switch attachment.Kind {
		case AttachmentText:
			if strings.TrimSpace(attachment.Text) != "" {
				return true
			}
		case AttachmentImage:
			if len(attachment.Data) > 0 {
				return true
			}
		}
	}
	return false
}

// composeUserText 把描述与【文本】附件拼成 user 消息的正文。
//
// 文本附件用 <file name="..."> 包裹：这是让模型分清「哪些是用户说的话、
// 哪些是材料原文」的最小代价——不包起来时，一份日志里的句子会被当成用户指令。
func composeUserText(description string, attachments []Attachment) string {
	var builder strings.Builder
	builder.WriteString(strings.TrimSpace(description))
	for _, attachment := range attachments {
		if attachment.Kind != AttachmentText {
			continue
		}
		body := strings.TrimSpace(attachment.Text)
		if body == "" {
			continue
		}
		builder.WriteString("\n\n<file name=\"")
		builder.WriteString(sanitizeName(attachment.Name))
		builder.WriteString("\">\n")
		builder.WriteString(body)
		builder.WriteString("\n</file>")
	}
	return builder.String()
}

// sanitizeName 去掉文件名里会破坏 <file name="..."> 标签的字符。
//
// 文件名来自用户磁盘（`a"b.txt` 完全可能），直接拼进 XML 风格的标签会把
// 标签撕成两半，模型的注意力就会被这点语法噪音吃掉。
func sanitizeName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "attachment"
	}
	replacer := strings.NewReplacer("\"", "'", "\n", " ", "\r", " ", "<", "(", ">", ")")
	return replacer.Replace(trimmed)
}

// imageParts 把图片附件转成 content part 列表。
//
// 非图片附件、空数据一律跳过：一份「有 name 没字节」的附件只会在请求体里
// 变成一张空图，某些端点会因此直接 400，而错误信息完全指不到附件那一行。
func imageParts(attachments []Attachment) []contentPart {
	parts := make([]contentPart, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.Kind != AttachmentImage || len(attachment.Data) == 0 {
			continue
		}
		parts = append(parts, contentPart{
			Type:     partTypeImageURL,
			ImageURL: &imageURL{URL: dataURL(attachment.MIME, attachment.Data)},
		})
	}
	return parts
}

// dataURL 把字节转成 data URI。
//
// MIME 为空时兜底成 image/png：OpenAI 兼容端点靠 MIME 判断如何解码，
// 空串会让部分实现当成 image/jpeg 处理，PNG 反而解不出来。
func dataURL(mime string, data []byte) string {
	normalized := strings.TrimSpace(mime)
	if normalized == "" {
		normalized = "image/png"
	}
	return "data:" + normalized + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// buildUserMessage 组装 user 消息。
//
// 【无图片时保持原协议】没有图片附件时返回纯字符串 content——与引入多模态之前
// 的请求体逐字节一致。理由：所有 OpenAI 兼容端点都接受数组形态，但不是所有
// 端点都接受「只有一个 text 片段的数组」；能不变就不变，少一类兼容性事故。
func buildUserMessage(description string, attachments []Attachment) chatMessage {
	body := composeUserText(description, attachments)
	images := imageParts(attachments)
	if len(images) == 0 {
		return textMessage("user", body)
	}
	parts := make([]contentPart, 0, len(images)+1)
	parts = append(parts, contentPart{Type: partTypeText, Text: body})
	parts = append(parts, images...)
	return partsMessage("user", parts)
}
