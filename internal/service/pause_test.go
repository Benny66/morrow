package service

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPauseStateUninterruptedFieldsJSON 锁定演示模式相关字段的 JSON 形状。
// 前端轮询 GetPauseState 时直接读这些键；改名会让 UI 静默读不到状态。
func TestPauseStateUninterruptedFieldsJSON(t *testing.T) {
	state := PauseState{
		Paused:                    true,
		Reason:                    "presentation",
		PresentationActive:        true,
		PresentationSource:        "manual",
		PresentationExitedAt:      1730000000,
		SuggestionVisible:         true,
		CaptureExclusionSupported: true,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("序列化 PauseState 失败: %v", err)
	}
	for _, want := range []string{
		`"presentation_active":true`,
		`"presentation_source":"manual"`,
		`"presentation_exited_at":1730000000`,
		`"suggestion_visible":true`,
		`"capture_exclusion_supported":true`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("PauseState JSON 缺少 %s，实际: %s", want, data)
		}
	}
}
