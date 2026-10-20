// Package ai 封装「一次性生成提醒计划」所需的最小 OpenAI 兼容客户端。
//
// 【为什么只用标准库 net/http】本包要做的事只有两件：发一个 JSON 请求、读一个
// JSON 响应。引入第三方 SDK 或 http 客户端库换不来任何能力，却要在 deps.yaml
// 多登记一条需要长期跟版本的直接依赖；而各家「OpenAI 兼容」端点的差异恰恰集中在
// SDK 抽象之外（错误体形状、是否支持 response_format），绕开 SDK 反而更省事。
//
// 【为什么 AI 只在这里出现一次】提醒必须离线可用。若每次提醒都调模型，断网即失效；
// 而用户对「该喝水了」这类文案的期待是稳定的，不需要每次重新生成。
// 故 AI 只负责「把自然语言描述翻译成一整套计划」，运行时完全靠本地定时器。
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"morrow/internal/apperr"
	"morrow/internal/model"
)

// 默认连接配置（单一真相：config 包的默认值从这里取，不要在别处再写一遍）。
const (
	// DefaultBaseURL 默认的 OpenAI 兼容端点（DeepSeek）。
	DefaultBaseURL = "https://api.deepseek.com/v1"
	// DefaultModel 默认模型。
	DefaultModel = "deepseek-v4.1-flash"
)

// RequestTimeout 是单次 AI 请求的超时。
//
// 生成一整套计划属于「长输出」，60 秒对云端模型是合理上界。超时后不再重试：
// 重试等于让用户再等一个 60 秒，而第一次超时通常说明网络或服务本身有问题。
const RequestTimeout = 60 * time.Second

// maxResponseBytes 是响应体的读取上限，防止异常端点灌爆内存。
const maxResponseBytes = 4 << 20

// Settings 是一次 AI 调用的连接配置。
type Settings struct {
	// BaseURL OpenAI 兼容端点，可带也可不带 /v1；带不带 /chat/completions 都能处理。
	BaseURL string
	// Model 模型名。
	Model string
	// APIKey 明文密钥。只在本进程内使用，绝不写日志、绝不放进错误详情。
	APIKey string
}

// Normalize 补齐空字段并去掉首尾空白与末尾斜杠。
func (s Settings) Normalize() Settings {
	out := s
	out.BaseURL = strings.TrimRight(strings.TrimSpace(out.BaseURL), "/")
	if out.BaseURL == "" {
		out.BaseURL = DefaultBaseURL
	}
	out.Model = strings.TrimSpace(out.Model)
	if out.Model == "" {
		out.Model = DefaultModel
	}
	out.APIKey = strings.TrimSpace(out.APIKey)
	return out
}

// PublicSettings 是暴露给设置页的脱敏配置。
//
// 【为什么单独一个类型】绑定方法的返回值会被序列化进前端、并可能出现在日志/截图里。
// 直接返回 Settings 就等于把明文 Key 交给渲染层——那是最容易被无意泄露的一处。
// 故读取路径只返回「有没有」与「脱敏串」，写入路径才接受明文。
type PublicSettings struct {
	// BaseURL 端点地址。
	BaseURL string `json:"base_url"`
	// Model 模型名。
	Model string `json:"model"`
	// APIKeySet 是否已配置密钥（前端据此提示"已保存，留空则不修改"）。
	APIKeySet bool `json:"api_key_set"`
	// APIKeyMasked 脱敏后的密钥（如 sk-****abcd），仅用于让用户确认填过哪一把。
	APIKeyMasked string `json:"api_key_masked"`
}

// NewPublicSettings 构造脱敏视图。
func NewPublicSettings(s Settings) PublicSettings {
	normalized := s.Normalize()
	return PublicSettings{
		BaseURL:      normalized.BaseURL,
		Model:        normalized.Model,
		APIKeySet:    normalized.APIKey != "",
		APIKeyMasked: MaskKey(normalized.APIKey),
	}
}

// MaskKey 把 API Key 脱敏成「前 3 位 + **** + 后 4 位」。
//
// 短于 8 位的串整体打码：一个 6 位串露出 7 个字符等于没打码。
func MaskKey(key string) string {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 8 {
		return "****"
	}
	return trimmed[:3] + "****" + trimmed[len(trimmed)-4:]
}

// ReminderDraft 是 AI 产出的一条提醒项（尚未落库，字段已归一化）。
type ReminderDraft struct {
	Title       string `json:"title"`
	Content     string `json:"content"`
	Kind        string `json:"kind"`
	IntervalMin int    `json:"interval_min"`
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
	DailyTarget int    `json:"daily_target"`

	// intervalMinSet 记录 JSON 里是否显式出现了 interval_min。
	//
	// 【为什么需要它】0 现在是合法的「每日单点」，而字段缺失的反序列化零值也是 0。
	// 没有这个标记，解析器无法区分「模型明确要求每天一次」与「模型漏填」——
	// 前者保留 0，后者必须回落到默认间隔，否则一条习惯会静默变成每天一次。
	intervalMinSet bool
}

// UnmarshalJSON 单独解出 interval_min 是否出现，其余字段走默认反序列化。
func (d *ReminderDraft) UnmarshalJSON(data []byte) error {
	type plain ReminderDraft
	aux := struct {
		*plain
		IntervalMin *int `json:"interval_min"`
	}{plain: (*plain)(d)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.IntervalMin != nil {
		d.IntervalMin = *aux.IntervalMin
		d.intervalMinSet = true
	}
	return nil
}

// Plan 是 AI 产出的一整套计划。
type Plan struct {
	Summary   string          `json:"summary"`
	Reminders []ReminderDraft `json:"reminders"`
}

// Client 是一个 OpenAI 兼容的 chat/completions 客户端。
type Client struct {
	settings Settings
	http     *http.Client
}

// NewClient 构造客户端（settings 会被归一化）。
func NewClient(settings Settings) *Client {
	return &Client{
		settings: settings.Normalize(),
		http:     &http.Client{Timeout: RequestTimeout},
	}
}

// Model 返回生效的模型名（供提示文案使用）。
func (c *Client) Model() string { return c.settings.Model }

// endpoint 拼出 chat/completions 的完整地址。
//
// 兼容两种用户写法：`https://api.deepseek.com/v1`（OpenAI 风格带版本段）与
// `https://xxx/v1/chat/completions`（用户直接粘了文档里的完整地址）。
// 只做后缀判断而不做 URL 解析：解析失败时仍能拼出一个可用的地址，报错留给 HTTP 层。
func (c *Client) endpoint() string {
	base := strings.TrimRight(c.settings.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

// chatRequest 是 chat/completions 的请求体（只声明用到的字段）。
type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

// chatResponse 是 chat/completions 的响应体（只声明用到的字段）。
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// doChat 发一次请求并返回首个 choice 的文本内容。
//
// 错误归一化（对排查很关键）：
//   - 网络/超时 → apperr（Message 面向用户，原始错误进 Detail）；
//   - HTTP 非 200 → 按状态码给出可操作的中文提示，响应体片段进 Detail；
//   - 【任何情况下都不把 API Key 写进 Message/Detail/日志】。
func (c *Client) doChat(ctx context.Context, messages []chatMessage, maxTokens int) (string, error) {
	if c.settings.APIKey == "" {
		return "", apperr.Validation("请先在设置页填写 AI API Key")
	}

	payload, err := json.Marshal(chatRequest{
		Model:       c.settings.Model,
		Messages:    messages,
		Temperature: 0.2, // 结构化输出要稳定，不要"创意"
		MaxTokens:   maxTokens,
		Stream:      false,
	})
	if err != nil {
		return "", apperr.Wrap(fmt.Errorf("序列化 AI 请求失败: %w", err))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return "", apperr.Wrap(fmt.Errorf("构造 AI 请求失败: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.settings.APIKey)

	resp, err := c.http.Do(req)
	if err != nil {
		wrapped := apperr.Wrap(err)
		wrapped.Message = "连接 AI 服务失败，请检查网络与 base_url"
		return "", wrapped
	}
	// 关闭响应体：这里的错误没有补救动作（连接已结束），显式忽略并说明原因。
	defer func() { _ = resp.Body.Close() }()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		wrapped := apperr.Wrap(fmt.Errorf("读取 AI 响应失败: %w", readErr))
		wrapped.Message = "读取 AI 响应失败，请稍后重试"
		return "", wrapped
	}

	if resp.StatusCode != http.StatusOK {
		return "", httpStatusError(resp.StatusCode, raw)
	}

	var parsed chatResponse
	if unmarshalErr := json.Unmarshal(raw, &parsed); unmarshalErr != nil {
		wrapped := apperr.Wrap(fmt.Errorf("解析 AI 响应失败: %w", unmarshalErr))
		wrapped.Message = "AI 返回的不是合法 JSON，请检查 base_url 是否为 OpenAI 兼容端点"
		wrapped.Detail = snippet(raw)
		return "", wrapped
	}
	// 部分兼容端点用 200 + error 体表达失败（如余额不足），必须显式识别。
	if parsed.Error != nil {
		wrapped := apperr.New(apperr.CodeInternal, "AI 服务返回错误："+parsed.Error.Message)
		wrapped.Detail = snippet(raw)
		return "", wrapped
	}
	if len(parsed.Choices) == 0 {
		return "", apperr.New(apperr.CodeInternal, "AI 没有返回任何内容，请稍后重试")
	}
	return parsed.Choices[0].Message.Content, nil
}

// httpStatusError 把非 200 状态码翻译成可操作的中文错误。
func httpStatusError(status int, raw []byte) *apperr.Error {
	message := fmt.Sprintf("AI 服务返回 HTTP %d", status)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		message = "AI 鉴权失败，请检查 API Key"
	case http.StatusNotFound:
		message = "AI 接口不存在，请检查 base_url（应指向 OpenAI 兼容端点）"
	case http.StatusBadRequest:
		// 400 是本仓最需要解释的一档：带图片时最常见的两个原因是
		// 「这个模型不支持图片」与「请求体超限」，端点自己会用英文说清楚，
		// 故把原始 message 放进 Detail 一起展示。
		message = "AI 拒绝了这次请求：可能是当前模型不支持图片，或内容超出限制（详见详情）"
	case http.StatusTooManyRequests:
		message = "AI 服务限流，请稍后再试"
	case http.StatusPaymentRequired:
		message = "AI 账户余额不足或未开通该模型"
	default:
		if status >= http.StatusInternalServerError {
			message = "AI 服务暂时不可用，请稍后再试"
		}
	}
	wrapped := apperr.New(apperr.CodeInternal, message)
	wrapped.Detail = snippet(raw)
	return wrapped
}

// retryInstruction 是第二次尝试时追加在用户消息后的格式要求。
//
// 【为什么要追加而不是原样重发】模型对自己上一条输出没有记忆，但对"上面那次格式
// 不对"的显式提示通常会立刻纠正（尤其是小模型）。这也是把重试次数压到 1 次的底气。
const retryInstruction = `上一次的回答不是合法 JSON，无法被程序解析。请【只】输出一个 JSON 对象，
不要任何解释文字、不要 Markdown 代码围栏、不要在 JSON 前后加任何字符。
JSON 必须以 { 开头、以 } 结尾，字段名与结构严格如下：
{"summary":"一句话总结","reminders":[{"title":"标题","content":"提醒正文","kind":"枚举值","interval_min":60,"window_start":"09:00","window_end":"18:00","daily_target":8}]}`

// buildSystemPrompt 生成 system prompt。
//
// kind 的枚举从 model.AllKinds() 取：提示词里再写一份清单就是第二处真相，
// 加枚举时忘了改提示词不会有任何报错，只会让新类型永远不出现。
func buildSystemPrompt() string {
	kindList := strings.Join(model.AllKinds(), "|")
	return `你是一个健康提醒计划生成器。用户会描述自己的工作场景与健康诉求，` +
		`你需要把它翻译成一套可执行的定时提醒计划。

输出要求（违反任何一条都会导致解析失败）：
1. 只输出一个 JSON 对象，不要 Markdown 代码围栏，不要在 JSON 前后加任何解释文字。
2. 结构必须是：
   {"summary":"一句话总结这套计划（不超过 40 字）","reminders":[{"title":"...","content":"...","kind":"...","interval_min":60,"window_start":"09:00","window_end":"18:00","daily_target":8}]}
3. kind 必须取以下之一：` + kindList + `。
4. interval_min 是整数分钟：0 表示每天只在 window_start 触发一次；大于 0 表示从 window_start 起按该间隔重复。
5. window_start / window_end 是 24 小时制的 HH:MM，且 window_end 必须晚于 window_start，不支持跨午夜。
6. daily_target 是整数，表示一天的目标次数；没有明确目标时给 0。
7. title 不超过 12 个汉字，content 不超过 40 个汉字，语气温和、具体、可执行（例如"起身倒一杯温水，慢慢喝完"）。
8. 提醒条数控制在 3–6 条：太少覆盖不到诉求，太多会变成噪音。
9. 用户描述里的时间、间隔、次数都是硬约束，必须遵守；没提到的部分按常识补全（默认工作时段 09:00–18:00）。
10. 用户可能直接给图片或文件（排班表、体检报告、日程截图等）：先读懂其中的时间与要求，再据此生成计划；
    材料里明确写出的时间优先于默认工作时段。`
}

// GeneratePlan 让模型把「自然语言描述 + 可选附件」翻译成一整套提醒计划。
//
// attachments 里的文本附件会被拼进 user 消息正文，图片附件会被转成 content part
// （同一份 user 消息的数组形态）。没有图片时请求体与引入多模态之前完全一致。
//
// 容错策略（三档，逐档收紧）：
//  1. 首次请求：system prompt 里给出严格 schema；
//  2. 解析失败 → 重试 1 次，这次在用户消息后追加更严厉的格式要求；
//  3. 仍失败 → 返回 apperr，并把模型原始返回的片段放进 Detail（供排查）。
//
// 【为什么不再多重试】每次重试都是一次网络往返加数十秒等待；用户在两分钟后拿到
// 第三份错误提示的体验，不如让他改一句描述重来。
// 【网络/鉴权错误不重试】那类失败重发多少次都一样，只会让用户多等一次超时。
// 【重试要重新组装同样的消息】图片是请求体的一部分：只重发文本却丢掉图片，
// 模型第二次就只能凭空猜，等于把重试变成了"换个问题再问一遍"。
func (c *Client) GeneratePlan(ctx context.Context, description string, attachments []Attachment) (Plan, error) {
	desc := strings.TrimSpace(description)
	if desc == "" && !hasAttachment(attachments) {
		return Plan{}, apperr.Validation("请先填写场景描述，AI 需要它来生成计划")
	}
	if c.settings.APIKey == "" {
		return Plan{}, apperr.Validation("请先在设置页填写 AI API Key")
	}

	// 只给附件、没写字时补一句中性引导语：content 不能是空串，
	// 而"发一张排班表"本身就是一次完整的表达，没必要逼用户再写一句。
	baseText := desc
	if baseText == "" {
		baseText = defaultAttachmentPrompt
	}

	systemPrompt := buildSystemPrompt()
	var lastRaw string
	var lastDetail string

	for attempt := 1; attempt <= 2; attempt++ {
		userText := baseText
		if attempt > 1 {
			userText = baseText + "\n\n" + retryInstruction
			slog.Warn("AI 计划解析失败，正在重试一次")
		}
		raw, err := c.doChat(ctx, []chatMessage{
			textMessage("system", systemPrompt),
			buildUserMessage(userText, attachments),
		}, 2048)
		if err != nil {
			return Plan{}, err
		}
		lastRaw = raw

		plan, parseErr := ParsePlanText(raw)
		if parseErr == nil {
			if strings.TrimSpace(plan.Summary) == "" {
				plan.Summary = fmt.Sprintf("已生成 %d 条提醒", len(plan.Reminders))
			}
			return plan, nil
		}
		lastDetail = fmt.Sprintf("第 %d 次解析失败：%v", attempt, parseErr)
		slog.Warn("AI 返回的计划无法解析", "attempt", attempt, "err", parseErr)
	}

	failure := apperr.New(apperr.CodeInternal, "AI 返回的计划格式不正确，请调整描述后重试")
	failure.Detail = lastDetail + "；模型原始返回片段：" + snippet([]byte(lastRaw))
	return Plan{}, failure
}

// TestConnection 验证 base_url / 模型名 / Key 三者可用，返回一句可展示的成功说明。
//
// 用 chat/completions 而不是 /models：部分兼容端点不实现 /models，而
// chat/completions 是「OpenAI 兼容」这个承诺里唯一被所有实现保证的入口。
func (c *Client) TestConnection(ctx context.Context) (string, error) {
	raw, err := c.doChat(ctx, []chatMessage{textMessage("user", "ping")}, 8)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("连接成功（模型 %s，返回 %d 字符）", c.settings.Model, len(strings.TrimSpace(raw))), nil
}
