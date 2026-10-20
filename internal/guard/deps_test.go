package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 4：依赖登记制（deps.yaml）双向校验
//   - 正向：go.mod 的直接 require（非 indirect）+ package.json 的 dependencies，
//           每一条都必须在 deps.yaml 登记。
//   - 反向：deps.yaml 里每个条目都必须真实存在于 go.mod 或 package.json。
// ---------------------------------------------------------------------------

// depsRegistry 是 deps.yaml 的解析结果。
type depsRegistry struct {
	goModules    map[string]bool
	frontendPkgs map[string]bool
}

func TestDependencyRegistryBidirectional(t *testing.T) {
	root := projectRoot()

	reg := parseDepsYAML(t, filepath.Join(root, "deps.yaml"))
	goDeps := parseGoDirectDeps(t, filepath.Join(root, "go.mod"))
	frontDeps := parseFrontendDeps(t, filepath.Join(root, "frontend", "package.json"))

	// 正向：清单里的直接依赖必须登记
	for m := range goDeps {
		if !reg.goModules[m] {
			t.Errorf("Go 直接依赖 %s 未在 deps.yaml 登记", m)
		}
	}
	for p := range frontDeps {
		if !reg.frontendPkgs[p] {
			t.Errorf("前端依赖 %s 未在 deps.yaml 登记", p)
		}
	}

	// 反向：登记项必须真实存在
	for m := range reg.goModules {
		if !goDeps[m] {
			t.Errorf("deps.yaml 登记了 Go 依赖 %s，但 go.mod 中无此直接依赖（僵尸条目/拼写）", m)
		}
	}
	for p := range reg.frontendPkgs {
		if !frontDeps[p] {
			t.Errorf("deps.yaml 登记了前端依赖 %s，但 package.json 中无此依赖（僵尸条目/拼写）", p)
		}
	}
}

// parseDepsYAML 解析 deps.yaml（行级，格式由本文件定义）。
func parseDepsYAML(t *testing.T, path string) *depsRegistry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 deps.yaml 失败: %v", err)
	}
	reg := &depsRegistry{goModules: map[string]bool{}, frontendPkgs: map[string]bool{}}
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch trimmed {
		case "go:":
			section = "go"
			continue
		case "frontend:":
			section = "frontend"
			continue
		}
		if section == "go" {
			if v := valueAfter(line, "module:"); v != "" {
				reg.goModules[v] = true
			}
		} else if section == "frontend" {
			if v := valueAfter(line, "package:"); v != "" {
				reg.frontendPkgs[v] = true
			}
		}
	}
	if len(reg.goModules) == 0 && len(reg.frontendPkgs) == 0 {
		t.Fatal("deps.yaml 解析到 0 个登记项——格式可能已变更，请同步更新护栏解析规则")
	}
	return reg
}

// parseGoDirectDeps 解析 go.mod 的直接依赖（排除 // indirect）。
func parseGoDirectDeps(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 go.mod 失败: %v", err)
	}
	out := map[string]bool{}
	// 只从 require 块/行里取依赖，并限定 module 路径形态（必须含 . 或 /）。
	// 不用「任意行 + ^\s*(\S+)\s+v...」的宽松正则：那会把 module 声明行
	// `module verifygen` 误当依赖——「verifygen」以 v 开头，恰好像版本号，
	// 导致任何以 v 开头的项目名被凭空报出一个叫 "module" 的未登记依赖。
	re := regexp.MustCompile(`^\s*([\w.\-~]+(?:/[\w.\-~]+)+)\s+v[\w.\-+]+`)
	inRequire := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "require ("):
			inRequire = true
			continue
		case inRequire && trimmed == ")":
			inRequire = false
			continue
		case strings.HasPrefix(trimmed, "require ") && !strings.HasPrefix(trimmed, "require ("):
			// 单行形式：require github.com/x/y v1.2.3
			body := strings.TrimPrefix(trimmed, "require ")
			if m := re.FindStringSubmatch(body); m != nil {
				out[m[1]] = true
			}
			continue
		}
		if !inRequire {
			continue
		}
		if strings.Contains(line, "// indirect") {
			continue
		}
		if m := re.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("go.mod 解析到 0 个直接依赖——解析规则可能已失效，请同步更新护栏解析规则")
	}
	return out
}

// parseFrontendDeps 解析 package.json 的 dependencies。
func parseFrontendDeps(t *testing.T, path string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 package.json 失败: %v", err)
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatalf("解析 package.json 失败: %v", err)
	}
	out := map[string]bool{}
	for k := range pkg.Dependencies {
		out[k] = true
	}
	if len(out) == 0 {
		t.Fatal("package.json 解析到 0 个 dependencies——可能已无直接依赖或解析失败")
	}
	return out
}

// valueAfter 提取 "key: value" 中 value 部分（去空白与引号）。
func valueAfter(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+len(key):])
	return strings.Trim(rest, `"'`)
}
