package main

import (
	"testing"
	"time"

	"morrow/internal/model"
	"morrow/internal/restmode"
)

// TestTrayWarningTextIsSnapshotCue 菜单栏那行预警必须与宠物气泡取自同一份文案。
//
// 【为什么单列一条】`app.go` 里曾独立写着「1 分钟后休息」：气泡改成按剩余分档后，
// 菜单栏若各写一份，就会出现"宠物说马上、菜单栏说 1 分钟后"的自相矛盾，
// 而这类漂移在代码里没有任何一处会报错。断言相等比检查字面量更耐用——
// 措辞以后怎么改，这条都不用动。
func TestTrayWarningTextIsSnapshotCue(t *testing.T) {
	base := time.Date(2026, 10, 22, 10, 0, 0, 0, time.Local)
	fire := base.Add(45 * time.Second)

	cases := []struct {
		desc string
		at   time.Time
		kind string
	}{
		{"stand 刚预警", base, model.KindStand},
		{"stand 剩 20 秒", base.Add(25 * time.Second), model.KindStand},
		{"stand 剩 5 秒", base.Add(40 * time.Second), model.KindStand},
		{"move 剩 20 秒", base.Add(25 * time.Second), model.KindMove},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			now := c.at
			machine := restmode.NewMachineWithClock(func() time.Time { return now })
			if !machine.EnterWarning(restmode.RestRequest{
				ReminderID: 1, Kind: c.kind, PlannedFireAt: fire.Unix(),
			}) {
				t.Fatal("测试前提：应能进入预警")
			}
			app := &App{restMode: machine}

			if got, want := app.trayStatusText(), machine.Snapshot().CueText; got != want {
				t.Errorf("菜单栏预警应与气泡同源，菜单栏=%q 快照=%q", got, want)
			}
		})
	}
}
