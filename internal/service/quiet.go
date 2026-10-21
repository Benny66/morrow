package service

// MarkQuietFire 标记某次触发为「安静投递」。
//
// 只进内存，不落库：待处理列表据此把 quiet_delivery 回传给 Alert.vue，
// 避免它把一条本应安静的提醒重新弹成全屏。应用重启后标记丢失，旧待处理会
// 按普通提醒展示；这与 B-11（顺延状态不持久化）是同一取舍。
func (s *Service) MarkQuietFire(fireID uint) {
	if s == nil || fireID == 0 {
		return
	}
	s.quietMu.Lock()
	defer s.quietMu.Unlock()
	if s.quietFires == nil {
		s.quietFires = map[uint]bool{}
	}
	s.quietFires[fireID] = true
}
