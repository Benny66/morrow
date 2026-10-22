package sit

import (
	"strings"
	"testing"
	"time"
)

// 毒舌只针对久坐行为与统计，不做身份/健康攻击，也不把玩笑落到医疗诊断上。
var forbiddenTemplateWords = []string{
	"丑", "蠢", "笨", "傻", "智商", "抑郁", "焦虑", "精神病", "有病",
	"吃药", "癌症", "诊断", "就医", "父母", "家人", "孩子", "胖", "瘦",
	"外貌", "残疾", "自杀",
}

var forbiddenAppNames = []string{
	"chrome", "safari", "vscode", "figma", "terminal", "slack",
	"微信", "钉钉", "飞书", "zoom", "teams",
}

func TestTemplatesRespectContentGuard(t *testing.T) {
	nudgeIDs := map[string]bool{}
	for tone, variants := range nudgeVariants {
		if len(variants) < 2 {
			t.Errorf("%s 软提醒模板不足 2 条", tone)
		}
		for _, variant := range variants {
			if nudgeIDs[variant.ID] {
				t.Errorf("软提醒模板 ID 重复: %s", variant.ID)
			}
			nudgeIDs[variant.ID] = true
			assertTemplateSafe(t, variant.Text)
			if !hasActionWord(variant.Text) {
				t.Errorf("软提醒模板缺少可执行动作: %q", variant.Text)
			}
		}
	}

	for tone, variants := range greetingVariants {
		for _, variant := range variants {
			assertTemplateSafe(t, variant.Text)
		}
		if greetingShort[tone] == "" {
			t.Errorf("%s 缺少短欢迎文案", tone)
		}
	}
	for _, text := range greetingShort {
		assertTemplateSafe(t, text)
	}
	for _, text := range breakStartTemplates {
		assertTemplateSafe(t, text)
	}
	for _, text := range muteConfirmTemplates {
		assertTemplateSafe(t, text)
	}
	for _, text := range toneConfirmTemplates {
		assertTemplateSafe(t, text)
	}

	for _, spec := range quoteTemplates {
		for tone, text := range spec.text {
			assertTemplateSafe(t, text)
			formatted := text
			for key, value := range map[string]string{
				"n": "30", "category": "写代码", "weekday": "周三", "total": "3",
				"skips": "2", "snoozes": "1",
			} {
				formatted = strings.ReplaceAll(formatted, "{"+key+"}", value)
			}
			if strings.Contains(formatted, "{") || strings.Contains(formatted, "}") {
				t.Errorf("%s/%s 记忆模板有未填充占位符: %q", spec.id, tone, text)
			}
			// 记忆句只做观察：软提醒气泡本身已经带了动作，这里再出现动作词就是复读命令。
			if hasActionWord(text) {
				t.Errorf("%s/%s 记忆模板重复了软提醒的动作指令: %q", spec.id, tone, text)
			}
		}
	}
}

func assertTemplateSafe(t *testing.T, text string) {
	t.Helper()
	if strings.TrimSpace(text) == "" {
		t.Error("模板不应为空")
		return
	}
	lower := strings.ToLower(text)
	for _, word := range forbiddenTemplateWords {
		if strings.Contains(text, word) {
			t.Errorf("模板命中硬禁区词 %q: %q", word, text)
		}
	}
	for _, app := range forbiddenAppNames {
		if strings.Contains(lower, app) {
			t.Errorf("模板出现应用名 %q: %q", app, text)
		}
	}
}

func hasActionWord(text string) bool {
	for _, word := range []string{"起来", "走两步", "活动", "晃晃", "伸个懒腰", "站起来"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func TestSelectNudgeTextAvoidsRecentTemplate(t *testing.T) {
	text, id := SelectNudgeText(60*time.Minute, ToneSassy, nil)
	if !strings.Contains(text, "60") || id == "" {
		t.Fatalf("软提醒文案/ID = %q/%q", text, id)
	}
	second, secondID := SelectNudgeText(60*time.Minute, ToneSassy, []string{NudgeTemplateID(ToneSassy)})
	if secondID == NudgeTemplateID(ToneSassy) || secondID == "" {
		t.Errorf("近期用过的模板不应重复，得到 %q", secondID)
	}
	if second == "" {
		t.Error("备用模板文案为空")
	}
	all := []string{"nudge.sassy.base", "nudge.sassy.2", "nudge.sassy.3"}
	fallback, fallbackID := SelectNudgeText(60*time.Minute, ToneSassy, all)
	if fallback == "" || fallbackID == "" {
		t.Errorf("所有模板都用过时应退回首条，得到 %q/%q", fallback, fallbackID)
	}
}

func TestToneConfirmations(t *testing.T) {
	for _, tone := range []Tone{ToneSassy, ToneLively, ToneGentle} {
		if MuteConfirmText(tone) == "" || ToneConfirmText(tone) == "" || BreakStartText(tone) == "" {
			t.Errorf("%s 缺少确认/回执文案", tone)
		}
	}
	if !strings.Contains(ToneConfirmText(ToneGentle), "好好说") {
		t.Errorf("温和确认 = %q", ToneConfirmText(ToneGentle))
	}
}
