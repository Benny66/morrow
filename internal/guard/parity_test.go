package guard

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 7：前端镜像与 Go 单一真相一致（parity）
//
// 背景：错误码 / 事件动作 / 页大小上下界三类常量，单一真相在 Go（internal/apperr、
// internal/event、internal/page），前端为了编译期可用各留了一份镜像。
//
// 镜像本身无可厚非——【无人看管的镜像】才是问题：Go 改了前端没跟上，
// 前端就在按一份过期的真相分流，且不会有任何报错。
//
// 本护栏把「二者必须一致」编译成会红的检查：
//   - 正向：Go 有的常量，前端镜像必须有且取值相等；
//   - 反向：前端多出的条目（Go 已删）也报错——否则镜像会退化成第二处真相。
//
// 命名约定（护栏据此配对，写错即解析不到 → Fatal 而非静默）：
//
//	Go 常量名 = goPrefix + Key + goSuffix        前端镜像 = `<tsObject> = { <Key>: <字面量> }`
//
// 铁律：解析到 0 个目标必须报错（护栏感知自己瞎了）。
// ---------------------------------------------------------------------------

func TestFrontendMirrorsMatchGoSourceOfTruth(t *testing.T) {
	mirrors := []struct {
		name     string
		goPkg    string
		tsFile   string
		tsObject string
		goPrefix string
		goSuffix string
	}{
		{
			name:     "错误码",
			goPkg:    "internal/apperr",
			tsFile:   "frontend/src/lib/invoke.ts",
			tsObject: "ErrorCode",
			goPrefix: "Code",
		},
		{
			name:     "事件动作",
			goPkg:    "internal/event",
			tsFile:   "frontend/src/lib/event.ts",
			tsObject: "EventAction",
			goPrefix: "Action",
		},
		{
			name:     "页大小上下界",
			goPkg:    "internal/page",
			tsFile:   "frontend/src/lib/page.ts",
			tsObject: "PageSize",
			goSuffix: "PageSize",
		},
		{
			// 应用级事件动作（main 包），如 ActionSecondInstance
			name:     "应用事件动作",
			goPkg:    ".",
			tsFile:   "frontend/src/lib/app.ts",
			tsObject: "AppEvent",
			goPrefix: "Action",
		},
	}

	root := projectRoot()
	for _, m := range mirrors {
		t.Run(m.name, func(t *testing.T) {
			goValues := parseGoConstValues(t, filepath.Join(root, m.goPkg), m.goPrefix, m.goSuffix)
			tsValues := parseTSObject(t, filepath.Join(root, m.tsFile), m.tsObject)

			// 正向：Go 有的，前端镜像必须有且相等
			for key, want := range goValues {
				got, ok := tsValues[key]
				if !ok {
					t.Errorf("%s：Go 侧 %s%s%s = %q，但前端镜像 %s 的 %s 中缺失——请在 %s 中补上（镜像已漂移）",
						m.name, m.goPrefix, key, m.goSuffix, want, m.tsFile, m.tsObject, m.tsFile)
					continue
				}
				if got != want {
					t.Errorf("%s：镜像不一致——%s.%s = %q，Go 侧 %s%s%s = %q。请同步 %s（单一真相在 Go）",
						m.name, m.tsObject, key, got, m.goPrefix, key, m.goSuffix, want, m.tsFile)
				}
			}
			// 反向：前端多出的条目（Go 已删/改名）——防止镜像变成第二处真相
			for key, got := range tsValues {
				if _, ok := goValues[key]; !ok {
					t.Errorf("%s：前端镜像 %s.%s = %q 在 Go 侧无对应常量（%s%s%s）——镜像已成第二处真相，请删除或改名",
						m.name, m.tsObject, key, got, m.goPrefix, key, m.goSuffix)
				}
			}
		})
	}
}

// parseGoConstValues 解析 Go 包内形如 <goPrefix><Key><goSuffix> 的常量，返回 Key → 取值的映射。
// 只认字面量（字符串/整数）常量；不匹配命名约定的常量（如包内私有常量）会被跳过，
// 因此新增内部常量不会误伤本护栏；但若一个都没解析到，说明约定或写法已变，直接 Fatal。
func parseGoConstValues(t *testing.T, dir, prefix, suffix string) map[string]string {
	t.Helper()
	files, err := parseDir(dir)
	if err != nil {
		t.Fatalf("解析 %s 失败: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("护栏在 %s 未解析到任何 .go 文件——写法可能已变更，请同步更新护栏解析规则", dir)
	}

	out := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, nameIdent := range vs.Names {
					key, ok := mirrorKey(nameIdent.Name, prefix, suffix)
					if !ok {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok {
						continue
					}
					val, ok := literalValue(lit)
					if !ok {
						continue
					}
					out[key] = val
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("护栏在 %s 未解析到形如 %s*%s 的导出常量——命名约定或写法可能已变更，请同步更新护栏解析规则",
			dir, prefix, suffix)
	}
	return out
}

// mirrorKey 按 <prefix>Key<suffix> 拆分常量名，返回 Key。
// 中间段必须非空且首字母大写（导出常量才需要前端镜像）。
func mirrorKey(name, prefix, suffix string) (string, bool) {
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return "", false
	}
	key := strings.TrimSuffix(strings.TrimPrefix(name, prefix), suffix)
	if key == "" || key[0] < 'A' || key[0] > 'Z' {
		return "", false
	}
	return key, true
}

// literalValue 返回字面量常量的规范化文本：字符串去引号，整数取原文本。
func literalValue(lit *ast.BasicLit) (string, bool) {
	switch lit.Kind {
	case token.STRING:
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case token.INT:
		return lit.Value, true
	default:
		return "", false
	}
}

// parseTSObject 解析前端镜像文件里 `export const <objName> = { Key: <字面量>, ... }` 的条目。
// 用受约束的行级正则而非 TS 解析器：不引第三方依赖；脆弱性由「解析不到即 Fatal」兜住。
func parseTSObject(t *testing.T, path, objName string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取前端镜像 %s 失败: %v", path, err)
	}

	startRe := regexp.MustCompile(`^export const ` + regexp.QuoteMeta(objName) + `\s*=\s*\{`)
	// 形如 `NotFound: 'not_found',` / `Max: 200,`（取值须为字符串或整数字面量）
	entryRe := regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(?:'([^']*)'|"([^"]*)"|(-?\d+))\s*,?\s*$`)

	out := map[string]string{}
	inBlock := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if startRe.MatchString(trimmed) {
				inBlock = true
			}
			continue
		}
		// 对象块的结束行（`} as const` / `}`）
		if strings.HasPrefix(trimmed, "}") {
			break
		}
		// 跳过注释行（// 与 /** */ 块内的 * 开头的行）
		if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		m := entryRe.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		switch {
		case m[2] != "":
			out[m[1]] = m[2]
		case m[3] != "":
			out[m[1]] = m[3]
		case m[4] != "":
			out[m[1]] = m[4]
		}
	}

	if !inBlock {
		t.Fatalf("在前端镜像 %s 中找不到 `export const %s = {`——写法可能已变更，请同步更新护栏解析规则",
			path, objName)
	}
	if len(out) == 0 {
		t.Fatalf("前端镜像 %s 的 %s 解析到 0 个条目——写法可能已变更，请同步更新护栏解析规则", path, objName)
	}
	return out
}
