package service

import (
	"morrow/internal/pause"
)

// PauseState 是暂停状态的可绑定视图（前端轮询用）。
type PauseState struct {
	// Paused 是否处于暂停状态。
	Paused bool `json:"paused"`
	// Reason 暂停原因（ReasonNone="none" 表示未暂停）。
	Reason string `json:"reason"`
	// Since 暂停开始时间（Unix 秒；0 表示未暂停）。
	Since int64 `json:"since"`
}

// ScreenRecordingStatus 是屏幕录制权限状态的可绑定视图（前端设置页用）。
type ScreenRecordingStatus struct {
	// Granted 是否已授权（Windows / Linux 恒 true 表示「不需要」）。
	Granted bool `json:"granted"`
	// AskedBefore 是否曾经问过用户（true = 至少走过一次说明窗，不再重复弹）。
	AskedBefore bool `json:"asked_before"`
	// PlatformNeedsPermission 是否需要走系统权限申请（macOS 10.15+ = true；
	// Windows / Linux = false）。
	PlatformNeedsPermission bool `json:"platform_needs_permission"`
}

// PauseService 是暂停信号的服务层封装。
type PauseService struct {
	provider pause.Provider
}

// NewPauseService 创建暂停信号服务。
func NewPauseService(provider pause.Provider) *PauseService {
	return &PauseService{provider: provider}
}

// GetState 返回当前暂停状态。
//
// 【Reason 归一化】State.Reason 的 Go 零值是 ""，而 pause.ReasonNone 是 "none"
// （见 provider.go 注释）。mock / 老代码路径可能返回 ""；这里统一归一化到
// "none"，让前端只需要认一个「未暂停」的字面量。
func (s *PauseService) GetState() PauseState {
	if s.provider == nil {
		return PauseState{Paused: false, Reason: string(pause.ReasonNone)}
	}
	state := s.provider.State()
	reason := string(state.Reason)
	if reason == "" {
		reason = string(pause.ReasonNone)
	}
	return PauseState{
		Paused: state.IsPaused(),
		Reason: reason,
		Since:  state.Since.Unix(),
	}
}
