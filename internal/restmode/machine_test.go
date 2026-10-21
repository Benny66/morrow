package restmode

import (
	"testing"
	"time"
)

// TestStateMachineTransitions 验证合法状态转换路径。
func TestStateMachineTransitions(t *testing.T) {
	m := NewMachine()

	// 初始为 Idle。
	if m.State() != StateIdle {
		t.Errorf("初始状态应为 Idle，实际 %s", m.State())
	}

	// Enter → Countdown（本 ticket 不做 Warning，见 008）。
	m.Enter(RestRequest{FireID: 1, Kind: "stand", Duration: 120 * time.Second})
	if m.State() != StateCountdown {
		t.Errorf("Enter 后应为 Countdown，实际 %s", m.State())
	}

	// Countdown → Done（倒计时结束）。
	m.Complete()
	if m.State() != StateDone {
		t.Errorf("Complete 后应为 Done，实际 %s", m.State())
	}

	// Done → Idle（终结事件回落到待处理状态；下一次 Enter 允许）。
	m.Reset()
	if m.State() != StateIdle {
		t.Errorf("Reset 后应为 Idle，实际 %s", m.State())
	}
}

// TestStateMachineSkip 验证用户主动跳过。
func TestStateMachineSkip(t *testing.T) {
	m := NewMachine()
	m.Enter(RestRequest{FireID: 42, Kind: "rest", Duration: 60 * time.Second})
	m.Skip()
	if m.State() != StateSkipped {
		t.Errorf("Skip 后应为 Skipped，实际 %s", m.State())
	}
}

// TestMachineRejectsSecondEnter 验证已在休息中时第二次 Enter 被拒绝（不覆盖当前会话）。
//
// 【为什么必须拒绝】如果允许覆盖，用户在 stand 休息到一半时收到 move 提醒，
// 当前 stand 会话的 FireID 会被丢掉、无法记完成；倒计时也会突然跳回 180 秒，
// 视觉上像"没做完又重来一次"。
func TestMachineRejectsSecondEnter(t *testing.T) {
	m := NewMachine()
	first := RestRequest{FireID: 1, Kind: "stand", Duration: 120 * time.Second}
	if !m.Enter(first) {
		t.Fatal("第一次 Enter 应成功")
	}
	second := RestRequest{FireID: 2, Kind: "move", Duration: 180 * time.Second}
	if m.Enter(second) {
		t.Error("休息中第二次 Enter 应返回 false（不覆盖当前会话）")
	}
	// 当前会话必须保持第一条。
	req := m.Current()
	if req.FireID != 1 || req.Kind != "stand" {
		t.Errorf("当前会话被覆盖：%+v", req)
	}
}

// TestMachineCompleteAfterIdleIsNoop 验证 Idle 状态下 Complete 不产生副作用。
//
// 【为什么单独测】前端可能因网络延迟收到一条过期的 Complete 事件；如果那时
// 机器已经 Reset 到 Idle，Complete 不应该把状态改成 Done（那会让下一次 Enter
// 前的 5 秒里 UI 显示「已完成」，用户看糊涂）。
func TestMachineCompleteAfterIdleIsNoop(t *testing.T) {
	m := NewMachine()
	m.Complete()
	if m.State() != StateIdle {
		t.Errorf("Idle 状态下 Complete 应被忽略，实际状态 %s", m.State())
	}
}

// TestMachineRemainingReflectsElapsed 验证 Remaining 随时间推进递减。
//
// 【为什么让机器持有 clock】Remaining 只在 Countdown 状态下有意义；把时间
// 计算收在 machine 里，前端只查一次拿快照，不必自己维护 startedAt 与本地时钟。
func TestMachineRemainingReflectsElapsed(t *testing.T) {
	base := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	clock := base
	m := NewMachine()
	m.now = func() time.Time { return clock }

	m.Enter(RestRequest{FireID: 1, Kind: "move", Duration: 180 * time.Second})
	if r := m.Remaining(); r != 180*time.Second {
		t.Errorf("刚开始 Remaining 应为 180s，实际 %v", r)
	}

	clock = base.Add(30 * time.Second)
	if r := m.Remaining(); r != 150*time.Second {
		t.Errorf("过 30 秒 Remaining 应为 150s，实际 %v", r)
	}

	clock = base.Add(200 * time.Second)
	// 超出总时长时 Remaining 夹在 0，不返回负数（前端拿到负数会渲染成 "-20s"）。
	if r := m.Remaining(); r != 0 {
		t.Errorf("超期后 Remaining 应夹到 0，实际 %v", r)
	}
}

// TestMachineIdleRemainingZero 验证非 Countdown 状态下 Remaining=0（避免前端拿到无意义数值）。
func TestMachineIdleRemainingZero(t *testing.T) {
	m := NewMachine()
	if r := m.Remaining(); r != 0 {
		t.Errorf("Idle 状态 Remaining 应为 0，实际 %v", r)
	}
	m.Enter(RestRequest{FireID: 1, Kind: "rest", Duration: 60 * time.Second})
	m.Complete()
	if r := m.Remaining(); r != 0 {
		t.Errorf("Done 状态 Remaining 应为 0，实际 %v", r)
	}
}

// TestSnapshotIsStableCopy 验证 Snapshot 返回快照而非内部引用。
//
// 【为什么单独测】如果 Snapshot 直接返回 *Session，前端绑定层的 JSON 序列化
// 会把 machine 内部状态暴露出去；改一下 current 字段会让上一次 Snapshot 也漂移。
func TestSnapshotIsStableCopy(t *testing.T) {
	m := NewMachine()
	m.Enter(RestRequest{FireID: 7, Kind: "stand", Duration: 120 * time.Second})
	snap1 := m.Snapshot()
	m.Skip()
	snap2 := m.Snapshot()
	if snap1.State == snap2.State {
		t.Error("两次 Snapshot 状态应不同（Skip 之后）")
	}
	if snap1.FireID != 7 {
		t.Errorf("第一次 Snapshot 应保留进入时的 FireID，实际 %d", snap1.FireID)
	}
}

// TestMachineThreadSafe 验证并发 Enter/Snapshot 不产生数据竞争。
//
// 【为什么必须测】引擎 goroutine 会在 dispatch 时 Enter；前端绑定的查询走另一
// 个 goroutine。裸字段读写会在 go test -race 下变红。
func TestMachineThreadSafe(t *testing.T) {
	m := NewMachine()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			_ = m.Snapshot()
			_ = m.State()
			_ = m.Remaining()
		}
		close(done)
	}()
	for i := 0; i < 20; i++ {
		m.Enter(RestRequest{FireID: uint(i), Kind: "rest", Duration: time.Second})
		m.Complete()
		m.Reset()
	}
	<-done
}
