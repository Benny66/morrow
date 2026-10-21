package reminder

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestFiredPayloadQuietDeliveryJSON 锁定安静投递标记的 JSON 形状。
// Alert.vue 依赖它区分「正常强提醒」与「顺延到上限后的安静投递」。
func TestFiredPayloadQuietDeliveryJSON(t *testing.T) {
	payload := FiredPayload{FireID: 1, QuietDelivery: true}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化 FiredPayload 失败: %v", err)
	}
	if !strings.Contains(string(data), `"quiet_delivery":true`) {
		t.Errorf("FiredPayload JSON 缺少 quiet_delivery=true，实际: %s", data)
	}
}
