package pause

// AuthorizationSnapshot 是「P1 敏感权限（专注 / 日历）」的可绑定视图。
//
// 三个字段合起来能回答设置页要显示的所有状态：
//   - SupportedOnOS + Granted：能不能读、有没有读；
//   - AskedBefore：用户是否已经被弹过说明窗（决定是否再弹）；
//   - Denied()：是不是「问过、被拒、平台支持」——这条决定了「已降级」提示的显示。
//
// 【为什么专注与日历共用一个结构】两者的降级逻辑一模一样（默认关、显式开、
// 拒绝后不再弹），拆两个结构会逼着 UI 层维护两份近似代码。用一个结构 + 两个
// 实例（Focus 与 Calendar）就够了。
type AuthorizationSnapshot struct {
	// Granted 是否已授权。
	Granted bool `json:"granted"`
	// AskedBefore 是否曾经问过用户（true = 至少走过一次说明窗，不再重复弹）。
	AskedBefore bool `json:"asked_before"`
	// SupportedOnOS 当前平台是否有这个能力（macOS 12+ 专注、macOS 日历）。
	SupportedOnOS bool `json:"supported_on_os"`
}

// Denied 返回「问过、被拒、平台支持」的组合。
//
// 【为什么这三个要一起判】
//   - SupportedOnOS=false：用户从没被授予过拒绝的机会，不是「拒了」；
//   - AskedBefore=false：还没问过，UI 应显示「点开关时会自动弹说明」；
//   - Granted=true：已经好了，不该显示降级。
func (a AuthorizationSnapshot) Denied() bool {
	return a.SupportedOnOS && a.AskedBefore && !a.Granted
}

// ShouldShowPrompt 返回是否应该弹一次说明窗。
//
// 只在平台支持、且【从未问过】时才弹——用户拒绝过就不再骚扰，
// 符合 ADR 0005 的「不弹二次请求」。
func (a AuthorizationSnapshot) ShouldShowPrompt() bool {
	return a.SupportedOnOS && !a.AskedBefore
}
