package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 护栏 5：生成锚点存在性断言
//   - make gen 依赖这些锚点定位注入位置；锚点删了会同时坏生成器与护栏。
//   - 这是"刻意的耦合"：单一真相，锚点清单在此与 scripts/gen.sh 保持一致。
// ---------------------------------------------------------------------------

// genAnchors 是生成器依赖的锚点清单（单一真相）。
// 与 scripts/gen.sh 的 ANCHORS 数组保持一致——新增锚点需两处同步。
var genAnchors = []struct {
	file   string
	marker string
}{
	{"internal/model/model.go", "// gen:model"},
	{"app.go", "// gen:import"},
	{"app.go", "// gen:bind"},
	{"frontend/src/router/index.ts", "// gen:route"},
	{"frontend/src/layouts/AppShell.vue", "// gen:menu"},
}

func TestGenAnchorsExist(t *testing.T) {
	root := projectRoot()
	for _, a := range genAnchors {
		data, err := os.ReadFile(filepath.Join(root, a.file))
		if err != nil {
			t.Fatalf("锚点所在文件 %s 不存在: %v", a.file, err)
		}
		if !strings.Contains(string(data), a.marker) {
			t.Errorf("生成锚点 %q 在 %s 中缺失——删了会坏 make gen 与护栏，请恢复", a.marker, a.file)
		}
	}
}
