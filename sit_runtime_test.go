package main

import (
	"testing"
	"time"

	"morrow/internal/sit"
)

func TestSitRuntimeResetAfterImport(t *testing.T) {
	app := &App{sitCfg: sitRuntimeSettings{ProfileEnabled: true}}
	r := newSitRuntime(app)
	r.sessionID = 42
	r.lastClosedSessionID = 41
	r.breakID = 7
	r.lastPersisted = 300
	r.lastPersistAt = time.Now()
	r.snoozeUntil = time.Now().Add(time.Minute)
	r.lastMemoryAttemptAt = time.Now()
	r.lastMemoryCheckAt = time.Now()
	r.appSessionID = 9
	r.lastAppID = "com.example.app"
	r.categoryAppID = "com.example.app"
	r.categoryCache = sit.CategoryCoding
	r.profileStatus = sit.ProfileStatusOK

	r.resetRuntimeState()

	if r.sessionID != 0 || r.lastClosedSessionID != 0 || r.breakID != 0 ||
		r.lastPersisted != 0 || !r.lastPersistAt.IsZero() || !r.snoozeUntil.IsZero() ||
		!r.lastMemoryAttemptAt.IsZero() || !r.lastMemoryCheckAt.IsZero() {
		t.Errorf("久坐运行态未重置: %+v", r)
	}
	if r.appSessionID != 0 || r.lastAppID != "" || r.categoryAppID != "" || r.categoryCache != "" {
		t.Errorf("应用采样态未重置: %+v", r)
	}
	if r.profileStatus != sit.ProfileStatusNoData {
		t.Errorf("画像开启时重置后状态 = %q，期望 no_data", r.profileStatus)
	}
	if snap := r.tracker.Snapshot(); snap.State != sit.StateIdle || snap.Focus != nil {
		t.Errorf("Tracker 未重置: %+v", snap)
	}
}
