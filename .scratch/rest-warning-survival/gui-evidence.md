# 005 实机走查证据（2026-10-23 09:51:54）

## 阶段 1：三档措辞在真机上的取值
- 第一档 >30s: state=countdown remaining=84s cue='起身站直，缓缓转头 5 秒，再耸肩放松 10 秒；重复几轮到倒计时结束。' 宠物DOM=bubble bubble--rest | 陪你休息 01:26 
- 第二档 ≤30s: state=warning remaining=27s cue='半分钟后该起身拉伸了' 宠物DOM=bubble bubble--warning | 半分钟后该起身拉伸了 
- 第三档 ≤10s: state=warning remaining=8s cue='马上该起身拉伸了' 宠物DOM=bubble bubble--warning | 马上该起身拉伸了

## 阶段 2：预警存活期间进入暂停（演示模式）→ 预警当轮撤销
- 暂停前：state=warning remaining=30s cue='半分钟后该起身拉伸了'
- 调用 SetPresentationMode(true) 作为暂停信号（reason=presentation，走的是同一条 paused 分支）
- 暂停后 25 秒：state=idle cue='' remaining=0
- 日志（撤销原因=暂停）：[]
- 判定：PASS

## 阶段 3：预警存活期间把那条提醒停用 → 预警撤销（等一个不再存在的 fire 没有意义）
- 停用前：state=warning remaining=50s cue='1 分钟后该起身拉伸了'
- 直接在 DB 里把 reminders.enabled 置 0（模拟用户在计划页关掉那条）
- 停用后 25 秒：state=idle remaining=0
- 日志：['time=2026-10-23T09:57:30.112+08:00 level=INFO msg=休息预警撤销：预告的触发已落定或不再存在 reminder_id=1 planned_fire_at=1792720680']
- 判定：PASS

## 阶段 4：一整轮正常循环之后，预警能力还在（僵尸态没回来）
- 到点进倒计时：state=countdown cue='起身站直，缓缓转头 5 秒，再耸肩放松 10 秒；重复几轮到倒计时结束。' remaining=119
- 用「提前结束→不算」收尾（SkipRestMode），避免在你屏幕上留满 2 分钟覆盖层
- 跳过后：state=idle
- 下一条的预警：state=warning remaining=29s cue='半分钟后该起身拉伸了'
- 判定：PASS

## 汇总：阶段2 PASS / 阶段3 PASS / 阶段4 PASS

## 阶段 5：持续输入 → 顺延到上限转安静投递（今天 8 次里 7 次走的这条）
- 预警在位：remaining=0s cue='马上该起身拉伸了'
- 持续输入 80 秒后：state=warning remaining=0 cue='马上该起身拉伸了'
- 日志：time=2026-10-23T10:01:50.113+08:00 level=INFO msg=自然停顿顺延到上限，转安静投递 scope=grid id=1 delay=40.001467459s max=30s quiet_delivery=true
- 日志：time=2026-10-23T10:01:50.131+08:00 level=INFO msg=提醒已送达 reminder=起身活动 fire_at=1792720860 rest_mode=false quiet_delivery=true
- 日志：time=2026-10-23T10:02:50.112+08:00 level=INFO msg=自然停顿顺延到上限，转安静投递 scope=grid id=1 delay=39.999691958s max=30s quiet_delivery=true
- 日志：time=2026-10-23T10:02:50.118+08:00 level=INFO msg=提醒已送达 reminder=起身活动 fire_at=1792720920 rest_mode=false quiet_delivery=true
- 最近一条触发记录状态：fired（安静投递应落 fired→待处理，而不是进休息模式）
- 判定：FAIL

## 阶段 5（重做）：安静投递之后预警撤销
第一次判红的用例设计错了：1 分钟间隔下，安静投递刚落定，下一条 fire 已进 60 秒窗口，
「该撤销」被「新的该亮」顶替。这次抓到安静投递后立刻把间隔改成 60 分钟，让撤销可观察。
- 预警在位：remaining=65s cue='起身站直，缓缓转头 5 秒，再耸肩放松 10 秒；重复几轮到倒计时结束。'
- 抓到安静投递：time=2026-10-23T10:03:50.112+08:00 level=INFO msg=自然停顿顺延到上限，转安静投递 scope=grid id=1 delay=40.000039875s max=30s quiet_delivery=true
- 立刻把 interval_min 改成 60：下一条 fire 远离预警窗口，此时预警若还挂着，唯一解释就是撤不掉
- 25 秒后：state=countdown remaining=38 cue='起身站直，缓缓转头 5 秒，再耸肩放松 10 秒；重复几轮到倒计时结束。'
- 引擎日志：time=2026-10-23T09:57:30.112+08:00 level=INFO msg=休息预警撤销：预告的触发已落定或不再存在 reminder_id=1 planned_fire_at=1792720680
- 判定：FAIL

## 阶段 5（第三次，判据对齐真实形态）：安静投递之后预警撤销
前两次判红都是脚本在抢跑：1 分钟间隔下「下一条该亮」会顶替「这一条该撤」，
末态 countdown 则是预警正确推进成倒计时。这次把间隔设成 3 分钟——
安静投递落定后，下一条 fire 还有 180 秒，预警窗口最早在 120 秒后才重新开，
所以中间有约 80 秒的真实空档；旧代码在这段时间里会一直挂着 remaining=0 的预警。
- 预警在位：state=warning remaining=49s cue='1 分钟后该起身拉伸了'
- 抓到安静投递：time=2026-10-23T10:03:50.112+08:00 level=INFO msg=自然停顿顺延到上限，转安静投递 scope=grid id=1 delay=40.000039875s max=30s quiet_delivery=true
- 停手 40 秒后（约 2 个 tick）：state=warning remaining=9 cue='马上该起身拉伸了'
- 引擎日志：time=2026-10-23T09:57:30.112+08:00 level=INFO msg=休息预警撤销：预告的触发已落定或不再存在 reminder_id=1 planned_fire_at=1792720680
- 判定：FAIL（旧代码此处是 warning + remaining=0 + 「1 分钟后该起身拉伸了」）

## 阶段 5（第四次，判据修正）：安静投递之后预警不再挂死
前三次都是脚本自己判错：1) 1 分钟间隔下「下一条该亮」顶替「这一条该撤」；
2) 末态 countdown 是预警正确推进成倒计时；3) 我用 f.tell() 取日志偏移，
而刚打开未读取时 tell() 恒为 0，于是抓到上一轮的陈旧「安静投递」行。

**判据换成这条**：安静投递落定后，快照要么 idle，要么是**指向更晚触发点的新预警**
（remaining_sec > 0）。卡死形态永远是 warning + remaining=0 + 「1 分钟后…」——
而新预警能否建起来本身就是撤销发生过的证据：EnterWarning 只接受 idle。
- 预警在位：state=warning remaining=50s cue='1 分钟后该起身拉伸了'
- 本轮抓到安静投递：None
- 判定：SKIP（本轮没造出安静投递，不判）

## 阶段 5（第四次，判据修正）：安静投递之后预警不再挂死
前三次都是脚本自己判错：1) 1 分钟间隔下「下一条该亮」顶替「这一条该撤」；
2) 末态 countdown 是预警正确推进成倒计时；3) 我用 f.tell() 取日志偏移，
而刚打开未读取时 tell() 恒为 0，于是抓到上一轮的陈旧「安静投递」行。

**判据换成这条**：安静投递落定后，快照要么 idle，要么是**指向更晚触发点的新预警**
（remaining_sec > 0）。卡死形态永远是 warning + remaining=0 + 「1 分钟后…」——
而新预警能否建起来本身就是撤销发生过的证据：EnterWarning 只接受 idle。
- 预警在位：state=warning remaining=49s cue='1 分钟后该起身拉伸了'
- 本轮抓到安静投递：time=2026-10-23T10:12:50.118+08:00 level=INFO msg=自然停顿顺延到上限，转安静投递 scope=grid id=1 delay=39.999893916s max=30s quiet_delivery=true
- 落定后 +20s：state=warning remaining=28 cue='半分钟后该起身拉伸了' → 合格
- 落定后 +40s：state=warning remaining=8 cue='马上该起身拉伸了' → 合格
- 判定：PASS

## 阶段 6：真实走引擎 paused 分支（空闲暂停，不是演示模式）
阶段 2 用的是演示模式，而进入演示模式在 app 层就有 CancelWarning 钩子（handleRestModeOnPresentationEnter），
所以那条并没有验证引擎里 `if paused { CancelWarning() }` 这一支——锁屏/会议/空闲靠的正是它。
这里把 pause_idle_enabled 打开、阈值 1 分钟：预警在位后完全停手，让空闲计时把提醒引擎推进暂停。
- 已改写临时 HOME 的 config：pause_idle_enabled=true, pause_idle_minutes=1（需重启实例生效）
- 预警在位：remaining=26s cue='半分钟后该起身拉伸了'（接下来 95 秒不注入任何输入，等空闲计时触发）
- 判定：SKIP（95 秒内没进入空闲暂停——期间若有键鼠输入就会推不动空闲计时）
