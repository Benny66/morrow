# 第二篇 · 发布包

> 正文：`desktop-pet-实战篇.md`
> 定位：技术干货 / 大白话 / 与第一篇（Wails v3 升级）互为姊妹篇

---

## 标题候选（8 选 1）

| # | 标题 | 反直觉 | 具体 | 关键词 | 传播 | 合计 |
|---|---|---|:--:|:--:|:--:|:--:|
| 1 | 做一个桌面宠物，真正难的不是画它 | 4 | 2 | 4 | 4 | **14** |
| 2 | 我做了只桌面宠物，它最开始在偷吃我的鼠标 | 5 | 4 | 5 | 5 | **19** ★推荐 |
| 3 | 一个 SVG 让我拖不动自己的宠物 | 5 | 4 | 4 | 4 | **17** |
| 4 | 透明 ≠ 点穿：桌面挂件的三个坑 | 4 | 3 | 5 | 4 | **16** |
| 5 | 240×380 里塞一只宠物和它的嘴 | 3 | 4 | 3 | 3 | **13** |
| 6 | 那行 `clientWidth` 恒为 0，害我查了 WebKit 源码 | 4 | 5 | 4 | 3 | **16** |
| 7 | 桌面宠物实现笔记：几何、命中、拖拽 | 2 | 4 | 4 | 2 | **12** |
| 8 | 桌宠不难画，难的是让它能拖、能藏、不挡人 | 3 | 3 | 4 | 3 | **13** |

**推荐 #2**：「偷吃我的鼠标」把最反直觉的技术事实（透明窗口照样吃点击）讲成了一个人格化的画面，桌宠关键词在里面，读者也猜不到答案。

**更稳妥备选 #4**：直接亮出核心结论，硬核读者吃这套。

**正文现在用的是 #1**。发布时建议换成 #2——正文开头已经埋了一句「它还干过一件我完全没想到的事：偷吃鼠标点击」当钩子，换标题不用改正文。

---

## 摘要（公众号「摘要」栏，55 字内）

> 透明窗口照样吃掉鼠标点击，SVG 没有布局盒，建窗坐标和 SetPosition 不是一套。三个坑，全不报错。

备用（更轻）：

> 宠物不难画。难的是它会把你的鼠标吃掉，而这件事一句日志都不会有。

---

## 封面建议

- 主体：小满（水滴 + 芽）站在桌面右下角
- 叠加：用半透明色块标出窗口矩形范围，宠物只占左上角一小块，剩下区域打叉
- 文案：**透明 ≠ 点穿**
- 副文案：桌面挂件的三个坑

不要用代码截图当封面，图太小看不清，且这类读者对代码截图已经免疫。

---

## 读者定位

- 主要：做桌面客户端 / Electron / Tauri / Wails 的工程师
- 次要：对"窗口、坐标、命中测试"这类系统级细节感兴趣的前端
- 不是给完全新手看的：需要知道什么是窗口坐标、什么是 DOM 事件

---

## 事实核对表

正文里每一个数字和结论都能在代码里查到。发之前我对过一遍：

| 文中说法 | 出处 |
|---|---|
| 窗口 240×380（1.0 倍），0.5 时 120×190 | `pet_window.go` `petBaseWidth/petBaseHeight`、`petWindowSize()` |
| 宠物 192×208，图集 8 列 9 行 1536×1872 | `frontend/src/lib/pet.ts` `FRAME_W/FRAME_H/PET_COLS/PET_ROWS` |
| 高度从 360 加到 380，加的是场景名那 20px | `pet_window.go` 注释「为什么从 360 加到 380」 |
| 0.5 倍时气泡字号只剩 6–7px | `docs/adr/0007-pet-bubble-on-demand-expansion.md` 备选方案二 |
| 缩放范围 0.5–2.0 | `internal/config/config.go` `MinPetScale/MaxPetScale` |
| 气泡窗口宽 240 | `pet_bubble_geometry.go` `petBubbleWindowWidth` |
| 9 个几何测试 | `pet_bubble_geometry_test.go`（`grep -c "^func Test"` = 9） |
| 副屏原点 1512 | `TestPetBubbleRectRespectsSecondaryScreenOrigin` |
| 贴边阈值 24px、露出 20px、轮询 500ms、宽限 3s | `pet_dock.go` 常量块 |
| 最小可见 48px、回落边距 24px | `pet_window.go` `petPositionMinVisible/petPositionMargin` |
| 点击判定 4px、按压过期 1200ms、兜底拖拽 3px | `frontend/src/lib/pet.ts` |
| 12fps / 失焦 8fps | `frontend/src/lib/pet.ts` `PET_FPS/PET_IDLE_FPS` |
| 设置轮询 4000ms | `frontend/src/lib/pet.ts` `PET_SETTINGS_POLL_MS` |
| SVG `clientWidth` 恒为 0，WebKit `Element.cpp` 无 SVG 分支 | `frontend/src/views/Pet.vue` 第 1643–1689 行注释（含源码路径） |
| 修法是 `pointer-events: none`，不是给 `.pet__art` 加宽高 | `Pet.vue` 第 1690–1725 行（`.pet__art` 无 width/height 声明） |
| 同一份前端产物 + `/#/pet` + `meta.bare` | `pet_window.go` 的 `URL`、`frontend/src/App.vue` 第 4–26 行、`router/index.ts` 第 72–77 行 |
| 窗口穿透要走 `ignoresMouseEvents` | macOS 系统行为，非本项目代码；与 `--wails-draggable` 互斥 |
| `drag.ts` 1px 启动原生拖拽 | `frontend/src/lib/pet.ts` `PET_CLICK_MAX_TRAVEL_PX` 注释 |
| `SuppressEvent` 掐 click/contextmenu/dblclick | 同上 |
| 录屏排除，Linux 为 no-op | `capture_protection.go` |
| 事件契约里没有宠物域 | `frontend/src/lib/pet.ts` `PET_SETTINGS_POLL_MS` 注释 |
| 长驻 goroutine 的 recover | `pet_dock.go` `watchPetPosition` |

**一处我刻意没写**：`pet_dock.go` 里那句 "240×360" 是上一版高度留下的旧注释。写进文章会误导，所以在正文里统一按 380 讲。如果哪天顺手理顺了，这行可以删。

---

## 发布检查

- [ ] 首图（封面）已生成，尺寸 900×383
- [ ] 代码块用公众号编辑器贴成等宽底色块，不要用截图
- [ ] 文中没有平台名、没有获取暗示、没有导流话术
- [ ] GitHub 链接只在开头和结尾各出现一次
- [ ] 与第一篇间隔 ≥ 2 天
- [ ] 留言区置顶准备：第一条放"透明窗口吃鼠标"的补充说明

---

## 留言区备用回复

**Q：为什么不用 `pointer-events` 直接让整个透明区穿透？**
A：（正文第三节已展开讲，一句话版）`pointer-events` 只管 DOM 层，窗口范围是原生窗口的事；要让窗口也穿透得走 `ignoresMouseEvents`，而它和"可拖动"直接冲突。

**Q：多屏下 `SetPosition` 是逻辑坐标还是物理坐标？**
A：逻辑坐标。Retina 缩放由框架处理，不用自己乘 2。但跨屏时两块屏的缩放比可能不同，这是我这套几何还没解决的部分，目前靠"位置必须落在某块屏的 WorkArea 内"兜住。

**Q：为什么不用 `WindowDidMove` 事件？**
A：v3 有这个事件，但不是所有平台后端都稳定 emit，我在当前环境没法离线验证。`Position()` 轮询是确定的，500ms 一次的开销可以忽略。

**Q：贴边收起和"自动隐藏"有什么区别？**
A：自动隐藏是产品行为（比如暂停时隐藏），贴边收起是用户手势的结果。前者按状态机走，后者按几何判断，两套逻辑不能混在一起。
