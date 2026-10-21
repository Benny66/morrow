package pause

import "testing"

// TestAuthorizationSnapshotReflectsDenied 验证 denied 状态能被 snapshot 透出。
//
// 【为什么单独测】Ticket 012 的核心是「用户拒绝后不再弹、UI 显示已降级」。
// Snapshot.Denied 是这条承诺的载体；一旦它算错，前端就会要么误导用户「请授权」，
// 要么悄悄把开关打开但检测器永远缺席。
func TestAuthorizationSnapshotReflectsDenied(t *testing.T) {
	cases := []struct {
		name       string
		granted    bool
		asked      bool
		wantDenied bool
		wantAsked  bool
	}{
		{"首次未询问", false, false, false, false},
		{"询问后拒绝", false, true, true, true},
		{"已授权", true, true, false, true},
		{"已授权但从未询问过（Windows 路径）", true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := AuthorizationSnapshot{
				Granted:       tc.granted,
				AskedBefore:   tc.asked,
				SupportedOnOS: true,
			}
			if snap.Denied() != tc.wantDenied {
				t.Errorf("Denied 判定：got %v, want %v", snap.Denied(), tc.wantDenied)
			}
			if snap.ShouldShowPrompt() != !tc.asked && tc.granted == false {
				t.Errorf("ShouldShowPrompt 逻辑不吻合：%+v", snap)
			}
		})
	}
}

// TestAuthorizationSnapshotOnUnsupportedOS 验证平台不支持时不显示降级提示。
//
// 【为什么】Windows/Linux 上根本没有专注/日历读取路径；「已降级」的提示
// 在这些平台上没意义（甚至会让用户以为是自己按错了什么）。
func TestAuthorizationSnapshotOnUnsupportedOS(t *testing.T) {
	snap := AuthorizationSnapshot{Granted: false, AskedBefore: false, SupportedOnOS: false}
	if snap.Denied() {
		t.Error("平台不支持时不应被算作 Denied（用户从未有过授权机会）")
	}
	if snap.ShouldShowPrompt() {
		t.Error("平台不支持时不应弹说明窗")
	}
}
