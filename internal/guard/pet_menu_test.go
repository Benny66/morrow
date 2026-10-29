package guard

// 护栏 29–30：宠物右键菜单的「分发接线」与「不抢拖拽」（openspec/changes/pet-context-menu）。
//
// 这两个失效都属于"不报错、不崩溃、只是点了没反应"：
//
//   - 菜单项加了但忘了写分发函数 → 点下去什么都不发生，日志里也没有痕迹；
//   - 菜单容器忘了 `--wails-draggable: no-drag` → `--wails-draggable: drag` 会从 `.pet`
//     继承下来，按下菜单项先启动窗口拖拽，随后的 click 被 wails 的 suppressEvent 吞掉。
//     气泡与气泡按钮当初都踩过同一个坑，两处注释里都留着记录。
//
// 两条都遵循 AGENTS.md 铁律 5：解析不到东西就 Fatal，不当成"通过"。

import (
	"regexp"
	"strings"
	"testing"
)

// petMenuActionPattern 匹配 lib/pet.ts 菜单表里的动作名。
//
// 依赖的是「表里每项都写 action: '名字'」这一形态；若将来改成从对象键推导，
// 下面的"解析到 0 个"分支会先响，提醒同步本护栏的解析规则。
var petMenuActionPattern = regexp.MustCompile(`action:\s*'([A-Za-z][A-Za-z0-9]*)'`)

func TestPetMenuActionsAreWired(t *testing.T) {
	vueSource := readPetVueStripped(t)
	libSource := stripComments(readFileOrFail(t, petLibRelativePath))

	matches := petMenuActionPattern.FindAllStringSubmatch(libSource, -1)
	if len(matches) == 0 {
		t.Fatalf("在 %s 里解析到 0 个菜单动作名——菜单表可能被改写成了别的形态"+
			"（当前匹配 action: '<名字>'）；护栏已经看不清代码，请同步更新本护栏的解析规则",
			petLibRelativePath)
	}

	seen := make(map[string]bool, len(matches))
	var missingHandler []string
	var missingCase []string
	for _, match := range matches {
		name := match[1]
		if seen[name] {
			t.Errorf("菜单动作 %q 在菜单表里出现了两次：多个菜单项指向同一动作，点哪个都一样", name)
			continue
		}
		seen[name] = true
		// 两件事必须同时成立，缺一个都是"点了没反应"：
		//   1. 处理函数存在（约定见 lib/pet.ts 的 PetMenuAction 注释）；
		//   2. runMenuItem 的 switch 里有对应 case。
		// 【第 2 条是本护栏第一版漏掉的】当时只查函数存在，于是删掉一个 case 之后
		// 护栏仍然是绿的——函数还在，但永远没人调它。只说"有函数"不等于"接上了线"。
		if !strings.Contains(vueSource, "function "+name+"(") {
			missingHandler = append(missingHandler, name)
		}
		if !strings.Contains(vueSource, "case '"+name+"':") {
			missingCase = append(missingCase, name)
		}
	}
	if len(missingHandler) > 0 {
		t.Errorf("这些菜单动作在 %s 里没有对应的处理函数：\n  %s\n"+
			"菜单项会渲染出来，但点了没有任何反应——这正是本轮要防的静默失效",
			petVueRelativePath, strings.Join(missingHandler, "\n  "))
	}
	if len(missingCase) > 0 {
		t.Errorf("这些菜单动作在 runMenuItem 的 switch 里没有对应的 case：\n  %s\n"+
			"处理函数写好了但永远调不到——点击会静默落空，且没有任何日志",
			strings.Join(missingCase, "\n  "))
	}
}

// petMenuCSSSelectorPrefix 是菜单容器的选择器前缀。
const petMenuCSSSelectorPrefix = ".pet__menu {"

func TestPetMenuExcludesWindowDrag(t *testing.T) {
	source := readPetVueStripped(t)

	block, ok := extractCSSBlock(source, petMenuCSSSelectorPrefix)
	if !ok {
		t.Fatalf("在 %s 里找不到 %s 规则块——菜单容器可能被改名或挪到了独立样式文件；"+
			"护栏已经看不清代码，请同步更新本护栏的解析规则",
			petVueRelativePath, strings.TrimSuffix(petMenuCSSSelectorPrefix, " {"))
	}
	if !strings.Contains(block, "--wails-draggable: no-drag") {
		t.Error("菜单容器没有声明 --wails-draggable: no-drag。" +
			"该属性会从 .pet 继承 drag，按下菜单项会先启动窗口拖拽，click 被 suppressEvent 吞掉，" +
			"表现是「菜单能弹出来，但点哪一项都没反应」")
	}
}

// extractCSSBlock 截取以 selector 开头的规则块的声明部分（不含选择器与花括号）。
//
// 只按第一个 `}` 收尾：本仓的 CSS 规则里没有嵌套块（@media 里的规则也各自闭合），
// 若将来引入嵌套语法，这里会截到内层大括号，由测试失败提醒改成更严谨的解析。
func extractCSSBlock(source, selector string) (string, bool) {
	index := strings.Index(source, selector)
	if index < 0 {
		return "", false
	}
	rest := source[index+len(selector):]
	end := strings.Index(rest, "}")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}
