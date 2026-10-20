package ai

import (
	"strings"
	"testing"

	"morrow/internal/model"
)

// TestParsePlanTextPlainJSON 断言最理想情况：模型直接吐合法 JSON。
func TestParsePlanTextPlainJSON(t *testing.T) {
	raw := `{"summary":"白天每小时喝水并定时起身","reminders":[
		{"title":"喝水","content":"起身倒一杯温水，慢慢喝完","kind":"water","interval_min":60,"window_start":"09:00","window_end":"18:00","daily_target":8}
	]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if plan.Summary == "" {
		t.Error("summary 不应为空")
	}
	if len(plan.Reminders) != 1 {
		t.Fatalf("提醒条数 = %d，期望 1", len(plan.Reminders))
	}
	got := plan.Reminders[0]
	if got.Title != "喝水" || got.Kind != model.KindWater {
		t.Errorf("字段不符: %+v", got)
	}
	if got.IntervalMin != 60 || got.DailyTarget != 8 {
		t.Errorf("数值字段不符: %+v", got)
	}
}

// TestParsePlanTextCodeFence 断言 ```json 围栏能被剥掉（小模型最常见的包装）。
func TestParsePlanTextCodeFence(t *testing.T) {
	raw := "好的，这是你的计划：\n\n```json\n" +
		`{"summary":"计划","reminders":[{"title":"起身","content":"站起来活动 5 分钟","kind":"stand","interval_min":45,"window_start":"09:00","window_end":"18:00","daily_target":0}]}` +
		"\n```\n\n希望有帮助！"
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plan.Reminders) != 1 || plan.Reminders[0].Title != "起身" {
		t.Fatalf("解析结果不符: %+v", plan)
	}
}

// TestParsePlanTextSurroundingNoise 断言无围栏但有前后废话时也能截出 JSON。
func TestParsePlanTextSurroundingNoise(t *testing.T) {
	raw := `当然可以！下面是为你定制的计划：
{"summary":"计划","reminders":[{"title":"走动","content":"起来走去接杯水","kind":"move","interval_min":90,"window_start":"09:00","window_end":"18:00","daily_target":4}]}
如果时间不合适，告诉我我再调整。`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plan.Reminders) != 1 || plan.Reminders[0].Kind != model.KindMove {
		t.Fatalf("解析结果不符: %+v", plan)
	}
}

// TestParsePlanTextFillsDefaults 断言字段缺失时给可用的默认值而不是零值。
func TestParsePlanTextFillsDefaults(t *testing.T) {
	raw := `{"reminders":[{"title":"喝水"}]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := plan.Reminders[0]
	if got.IntervalMin != defaultIntervalMin {
		t.Errorf("interval_min = %d，期望回落 %d", got.IntervalMin, defaultIntervalMin)
	}
	if got.WindowStart != defaultWindowStart || got.WindowEnd != defaultWindowEnd {
		t.Errorf("时段 = %s–%s，期望回落到 %s–%s",
			got.WindowStart, got.WindowEnd, defaultWindowStart, defaultWindowEnd)
	}
	if got.Kind != model.KindCustom {
		t.Errorf("kind = %q，期望回落 %q", got.Kind, model.KindCustom)
	}
	if got.Content != got.Title {
		t.Errorf("正文为空时应回落到标题，实际 %q", got.Content)
	}
}

// TestParsePlanTextNormalizesKind 断言非法 kind 一律落到 custom，且大小写不敏感。
func TestParsePlanTextNormalizesKind(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"WATER", model.KindWater},
		{" stand ", model.KindStand},
		{"unknown", model.KindCustom},
		{"", model.KindCustom},
	}
	for _, c := range cases {
		raw := `{"reminders":[{"title":"x","kind":"` + c.input + `"}]}`
		plan, err := ParsePlanText(raw)
		if err != nil {
			t.Fatalf("kind=%q 解析失败: %v", c.input, err)
		}
		if got := plan.Reminders[0].Kind; got != c.want {
			t.Errorf("kind %q 归一化为 %q，期望 %q", c.input, got, c.want)
		}
	}
}

// TestParsePlanTextRejectsBadWindow 断言非法/反向时段回落到常规时段。
func TestParsePlanTextRejectsBadWindow(t *testing.T) {
	raw := `{"reminders":[{"title":"x","window_start":"18:00","window_end":"09:00"}]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := plan.Reminders[0]; got.WindowStart != defaultWindowStart || got.WindowEnd != defaultWindowEnd {
		t.Errorf("反向时段 = %s–%s，期望回落默认", got.WindowStart, got.WindowEnd)
	}
}

// TestParsePlanTextPadsClock 断言 "9:00" 被归一化成 "09:00"（前端时刻选择器只认两位）。
func TestParsePlanTextPadsClock(t *testing.T) {
	raw := `{"reminders":[{"title":"x","window_start":"9:00","window_end":"18:30"}]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := plan.Reminders[0]
	if got.WindowStart != "09:00" || got.WindowEnd != "18:30" {
		t.Errorf("时段 = %s–%s，期望 09:00–18:30", got.WindowStart, got.WindowEnd)
	}
}

// TestParsePlanTextDeduplicates 断言同一标题同时段的重复项被合并。
func TestParsePlanTextDeduplicates(t *testing.T) {
	raw := `{"reminders":[
		{"title":"喝水","window_start":"09:00","window_end":"18:00"},
		{"title":"喝水","window_start":"09:00","window_end":"18:00"},
		{"title":"喝水","window_start":"10:00","window_end":"18:00"}
	]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plan.Reminders) != 2 {
		t.Errorf("去重后条数 = %d，期望 2", len(plan.Reminders))
	}
}

// TestParsePlanTextClampsLongText 断言超长标题按字符截断（不是按字节）。
func TestParsePlanTextClampsLongText(t *testing.T) {
	longTitle := strings.Repeat("久", 80)
	raw := `{"reminders":[{"title":"` + longTitle + `"}]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if got := plan.Reminders[0].Title; len([]rune(got)) != maxTitleRunes {
		t.Errorf("标题字符数 = %d，期望 %d", len([]rune(got)), maxTitleRunes)
	}
}

// TestParsePlanTextErrors 断言真正解析不出来时返回错误（调用方据此决定重试）。
func TestParsePlanTextErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"完全没有 JSON", "抱歉，我无法完成这个请求。"},
		{"JSON 语法错误", `{"summary":"计划","reminders":[{"title":"喝水",}]}`},
		{"提醒项为空", `{"summary":"计划","reminders":[]}`},
		{"空串", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ParsePlanText(c.raw); err == nil {
				t.Error("期望返回错误，实际解析成功")
			}
		})
	}
}

// TestNormalizeSettings 断言空配置被补齐为默认端点与默认模型。
func TestNormalizeSettings(t *testing.T) {
	got := Settings{BaseURL: "  ", Model: ""}.Normalize()
	if got.BaseURL != DefaultBaseURL {
		t.Errorf("base_url = %q，期望默认 %q", got.BaseURL, DefaultBaseURL)
	}
	if got.Model != DefaultModel {
		t.Errorf("model = %q，期望默认 %q", got.Model, DefaultModel)
	}

	trailing := Settings{BaseURL: "https://example.com/v1//"}.Normalize()
	if trailing.BaseURL != "https://example.com/v1" {
		t.Errorf("末尾斜杠未去掉: %q", trailing.BaseURL)
	}
}

// TestMaskKey 断言脱敏不会泄露完整密钥。
func TestMaskKey(t *testing.T) {
	if got := MaskKey(""); got != "" {
		t.Errorf("空密钥脱敏为 %q，期望空串", got)
	}
	if got := MaskKey("sk-123"); got != "****" {
		t.Errorf("短密钥脱敏为 %q，期望全掩", got)
	}
	full := "sk-abcdefghijklmn"
	masked := MaskKey(full)
	if masked == full {
		t.Fatal("脱敏后与原文相同")
	}
	if strings.Contains(masked, "defghij") {
		t.Errorf("脱敏结果泄露了中段: %q", masked)
	}
	if !strings.HasPrefix(masked, "sk-") || !strings.HasSuffix(masked, "klmn") {
		t.Errorf("脱敏结果应保留首尾以便用户辨认: %q", masked)
	}
}

// TestNewPublicSettings 断言公开视图不包含明文密钥。
func TestNewPublicSettings(t *testing.T) {
	pub := NewPublicSettings(Settings{BaseURL: DefaultBaseURL, Model: DefaultModel, APIKey: "sk-secret-key-1234"})
	if !pub.APIKeySet {
		t.Error("api_key_set 应为 true")
	}
	if strings.Contains(pub.APIKeyMasked, "secret") {
		t.Errorf("脱敏串泄露了密钥内容: %q", pub.APIKeyMasked)
	}
	empty := NewPublicSettings(Settings{})
	if empty.APIKeySet {
		t.Error("未配置密钥时 api_key_set 应为 false")
	}
}

// TestClientEndpoint 断言两种用户写法都能拼出正确地址。
func TestClientEndpoint(t *testing.T) {
	const want = "https://api.deepseek.com/v1/chat/completions"
	cases := []struct {
		input string
		want  string
	}{
		{"https://api.deepseek.com/v1", want},
		{"https://api.deepseek.com/v1/", want},
		{"https://api.deepseek.com/v1/chat/completions", want},
		{"", DefaultBaseURL + "/chat/completions"},
	}
	for _, c := range cases {
		client := NewClient(Settings{BaseURL: c.input})
		if got := client.endpoint(); got != c.want {
			t.Errorf("endpoint(%q) = %q，期望 %q", c.input, got, c.want)
		}
	}
}

// TestParsePlanTextKeepsZeroInterval 断言显式的 interval_min=0 被保留为「每日单点」，
// 不会被当成漏字段回落成 60 分钟。零值是合法输入，不能与「字段缺失」混为一谈。
func TestParsePlanTextKeepsZeroInterval(t *testing.T) {
	raw := `{"reminders":[{"title":"吃药","interval_min":0,"window_start":"16:00","window_end":"18:00"}]}`
	plan, err := ParsePlanText(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	got := plan.Reminders[0]
	if got.IntervalMin != 0 {
		t.Errorf("interval_min = %d，期望保留 0（每日单点）", got.IntervalMin)
	}
}

// TestBuildSystemPromptHasNoOneOffRule 断言 Plan 页提示词不再包含一次性提醒规则。
// 一次性提醒改由快速添加入口承载；两套提示词同时教模型「只做这一次」会互相矛盾。
func TestBuildSystemPromptHasNoOneOffRule(t *testing.T) {
	if strings.Contains(buildSystemPrompt(), "once") {
		t.Error("Plan 页 system prompt 仍包含 once 规则")
	}
}

// TestBuildSystemPromptListsAllKinds 断言提示词里的 kind 枚举来自单一真相。
//
// 这里守的是一条「没有症状的失效」：提示词里手写一份 kind 清单，加枚举时忘了同步，
// 模型就永远不会产出新类型，而不会有任何报错。
func TestBuildSystemPromptListsAllKinds(t *testing.T) {
	prompt := buildSystemPrompt()
	for _, kind := range model.AllKinds() {
		if !strings.Contains(prompt, kind) {
			t.Errorf("system prompt 未列出 kind %q（应来自 model.AllKinds）", kind)
		}
	}
}
