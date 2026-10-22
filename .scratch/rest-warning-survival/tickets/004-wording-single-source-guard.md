# 004 — 护栏：预警措辞只允许一个出处

**Blocked by**: 003

**状态**: 已完成（2026-10-22；单测 + 护栏 + 注入证据；GUI 走查转 005）

> 规格：`docs/specs/2026-10-22-rest-warning-survival.md` 的 WS-18、§5.2 的 WS-T12。
> 依据：AGENTS.md 铁律 1（单一真相）与铁律 5（护栏必须"感知自己瞎了"）。
> ADR 0012 的「后果」里点名了旧漂移样本：`app.go` 那句写死的「1 分钟后休息」。

## 目标

把"预警措辞只能在 `internal/restmode` 一处定义"从口头约定变成会红的检查：
今后谁在 `app.go`、前端或任何别处再手写一句「N 分钟后…」，`make lint` 当场拦住，
而不是等到某个用户对着挂了半小时的气泡提问。

## 涉及层

- [x] 逻辑层（护栏本体）：`internal/guard/warning_wording_test.go`（新）
  - 判据：全仓源码里的**字符串字面量**（用 `go/parser` + `ast.BasicLit` 取，避免命中注释）
    凡含「分钟后」「马上」这类预警措辞的，其所在文件必须位于 `internal/restmode/`。
  - 覆盖范围：Go 侧 `parseDir` 递归到的目录 + 前端 `frontend/src/**/*.{ts,vue}`。
    前端那半边用文本扫描即可——它本来就是"禁止出现"的一侧，不需要 AST。
  - 测试文件与 `.scratch`/`docs` 排除：措辞会在验收记录里被引用，不是违规。
  - 复用 `internal/guard/parse.go` 的 `projectRoot()` / `parseDir()`；
    若需要"递归整仓"的新辅助，加在 `parse.go` 里，别在测试文件内各写一份遍历。
  - **铁律 5 的两条硬要求**：
    1. 在 `internal/restmode/` 内解析到的预警措辞为 0 条 → `t.Fatal`，
       报错文案附「写法可能已变更，请同步更新护栏解析规则」；
       （即：`warningCueFor` 不存在、或 12 句被重构掉时，护栏不许静默通过。）
    2. 越界命中时报错必须带上**文件名 + 命中字面量**，让人一眼知道要删哪句。
- [x] 测试（自证有效性）
  - WS-T12a：临时在 `app.go` 插入 `return "1 分钟后休息"` → 护栏必须变红；
  - 临时把 `machine.go` 的 `warningCueFor` 改名或清空措辞 → 护栏必须因"解析到 0 条"变红；
  - 两处注入都要在验证后完整还原（`git diff` 只剩护栏本身与 003 的改动）。
  - 这两次注入是交付证据，写进本票的「实现记录」，没有它们就等于没测。
- [x] 文档：`AGENTS.md` 铁律 1 的举例段里已列出若干唯一真相（模型清单、依赖清单、appdir、契约镜像），
  本票补一行「预警措辞唯一真相在 `internal/restmode` 的措辞函数」；`docs/map.md` 增加该护栏条目。

## 验收标准

- `make lint` / `make test` 全绿。
- 上述两次"故意注入错误值"各自让护栏变红，且报错文案能指到具体文件与字面量。
- 全仓扫描的命中清单只剩 `internal/restmode/machine.go` 一个文件。

## 边界

- 不做逐秒措辞、不改 003 的措辞内容（本票只锁"只能有一处"）。
- 不把措辞搬进配置文件或 DB——它属于内置内容，与 `cue.go` 的「引导动作不调 AI」同一承诺。
- 不改 `guard/parity_test.go` 的既有 Go/TS 常量对账范围（措辞不进镜像）。

## 实现记录（2026-10-22）

- 护栏：新增 `internal/guard/warning_wording_test.go::TestWarningWordingHasSingleSource`。
  Go 侧用 `go/parser` + `ast.BasicLit` 只取**字符串字面量**（注释不参与，避免说明文字误报），
  前端 `.ts/.vue` 先剥注释再按成对引号取内容；`_test.go` 与 `docs/`、`.scratch/`、
  `node_modules/`、`dist/`、`build/` 一并排除——测试必须能写出期望字面量当独立真相源
  （`warning_test.go` 的三档表就是干这个的），把它算成越界等于自缚。
- 判据两头：越界命中报错并列出**文件路径 + 命中的字面量**；
  `internal/restmode` 内命中数低于下限 12（三档 × 四分支）时 `t.Fatalf`
  「护栏已经看不清代码……不要靠调低下限蒙过去」（铁律 5）。
- 一次真实的误报修正：初版特征写成 `分钟后|马上`，第一跑就把久坐软提醒里合法的
  「好，10 分钟后再提醒你」（`sit_runtime.go`）判成越界。
  改为锚句首 `^(1 分钟后|半分钟后|马上)`——预警措辞的形态就是三档句首，
  软提醒那种"数字 + 分钟后"的中止确认句自然出局。代价已在护栏注释里写明：
  将来给预警文案加前缀装饰需要同步放宽这条正则。
- **注入证据**（两次都验完还原）：
  - 在 `app.go` 写回 `return "1 分钟后休息"` → 护栏红，报错为
    `app.go（1 分钟后休息）`，同时 `TestTrayWarningTextIsSnapshotCue` 一起红；
  - 把 `machine.go` 的 12 句换成 `cue_stand_a` 之类占位符（模拟写法大改）→ 护栏红，
    报 `internal/restmode 里只解析到 0 条预警措辞，低于下限 12 条`。
    这一步证明护栏不是"永远通过"的摆设。
- 文档：`AGENTS.md` 铁律 1 的唯一真相清单补一条预警措辞；`docs/map.md` 的 `docs/adr/` 行附近
  补本护栏条目。
- 验证：`make lint`（含 guard 包）与 `make test` 全绿。

