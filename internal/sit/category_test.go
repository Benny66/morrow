package sit

import "testing"

func TestCategoryFor(t *testing.T) {
	cases := []struct {
		platform  string
		appID     string
		isMeeting bool
		want      Category
	}{
		{"darwin", "com.microsoft.VSCode", false, CategoryCoding},
		{"darwin", "com.figma.Desktop", false, CategoryDesign},
		{"darwin", "com.google.Chrome", false, CategoryBrowsing},
		{"darwin", "com.tencent.xinWeChat", false, CategoryCommunication},
		{"darwin", "us.zoom.xos", true, CategoryCommunication},
		{"darwin", "com.spotify.client", false, CategoryMedia},
		{"darwin", "com.example.unknown", false, CategoryOther},
		{"windows", "Code", false, CategoryCoding},
		{"windows", "chrome", false, CategoryBrowsing},
		{"windows", "wechat", false, CategoryCommunication},
		{"windows", "unknown-app", false, CategoryOther},
		{"linux", "anything", false, CategoryOther},
	}
	for _, c := range cases {
		if got := CategoryFor(c.platform, c.appID, c.isMeeting); got != c.want {
			t.Errorf("CategoryFor(%q,%q,%v) = %q，期望 %q", c.platform, c.appID, c.isMeeting, got, c.want)
		}
	}
}

func TestAppDisplayNameAndSelf(t *testing.T) {
	if got := AppDisplayName("com.microsoft.VSCode"); got != "VS Code" {
		t.Errorf("VS Code 展示名 = %q", got)
	}
	if got := AppDisplayName("com.example.unknown"); got != "com.example.unknown" {
		t.Errorf("未知应用应展示原始标识，得到 %q", got)
	}
	if !IsSelfApp("darwin", "io.github.bennywen.morrow") {
		t.Error("macOS 应识别 Morrow 自身")
	}
	if !IsSelfApp("windows", "Morrow") {
		t.Error("Windows 应识别 Morrow 自身")
	}
	if IsSelfApp("darwin", "com.google.Chrome") {
		t.Error("Chrome 不应被当成 Morrow")
	}
	if !IsSystemUIApp("darwin", "com.apple.loginwindow") ||
		!IsSystemUIApp("darwin", "com.apple.ScreenSaver.Engine") ||
		!IsSystemUIApp("windows", "LockApp.exe") {
		t.Error("登录/锁屏界面应被排除")
	}
	if IsSystemUIApp("darwin", "com.google.Chrome") || IsSystemUIApp("linux", "com.apple.loginwindow") {
		t.Error("普通应用/非目标平台不应被当成系统界面")
	}
}
