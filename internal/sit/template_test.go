package sit

import (
	"strings"
	"testing"
	"time"
)

func TestNudgeTextTones(t *testing.T) {
	texts := []string{
		NudgeText(60*time.Minute, ToneSassy),
		NudgeText(60*time.Minute, ToneLively),
		NudgeText(60*time.Minute, ToneGentle),
	}
	for _, text := range texts {
		if !strings.Contains(text, "60") {
			t.Errorf("软提醒应带上时长: %q", text)
		}
		if strings.Contains(text, "Chrome") || strings.Contains(text, "Code") {
			t.Errorf("软提醒不应出现应用名: %q", text)
		}
	}
	if texts[0] == texts[1] || texts[1] == texts[2] {
		t.Errorf("三档语气不应产出相同文案: %v", texts)
	}
}

func TestGreetingText(t *testing.T) {
	text := GreetingText(3*time.Minute, ToneGentle)
	if !strings.Contains(text, "3") {
		t.Errorf("欢迎回来应带休息时长: %q", text)
	}
	short := GreetingText(0, ToneSassy)
	if short == "" {
		t.Error("没有可测时长时也应有欢迎文案")
	}
}

func TestBubbleActions(t *testing.T) {
	nudge := BubbleActions(BubbleNudge)
	if len(nudge) != 3 || nudge[0] != ActionStartBreak || nudge[1] != ActionSnooze10 || nudge[2] != ActionMore {
		t.Errorf("软提醒动作 = %v", nudge)
	}
	greeting := BubbleActions(BubbleGreeting)
	if len(greeting) != 2 || greeting[0] != ActionDismiss || greeting[1] != ActionOpenApp {
		t.Errorf("欢迎动作 = %v", greeting)
	}
}
