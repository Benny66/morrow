<script lang="ts" setup>
import { onMounted } from 'vue'
// 绑定路径要与 lib/page.ts 的「前端镜像」配套，见下。
//
// 【v3 改了绑定落点，这是迁移时漏掉的一处】v2 的产物在 frontend/wailsjs/go/main/App，
// 且导出的方法名就是 Go 侧的名字；v3 的产物在 frontend/bindings/<go module 名>/app.ts
// （目录名取自 go.mod 的 module，不是应用名），文件名固定为 app.ts。
//
// 下面 import 里的路径段是模块名占位符，由 scripts/init.sh 在实例化时替换成项目名。
// 本模板文件位于前端工程之外，写错了这里不会被 vue-tsc 检查；出错的是生成出来的页面，
// 且报错指向 import 行。写法与 frontend/src/views/Settings.vue 同源。
//
// 【编辑本模板时注意】注释里不要写会被替换的字面量。会替换的有两类：
//   1. 模块名占位符——就是下面 import 里那一段路径，由 init.sh 换成项目名；
//   2. 模板自带的示例模块名（两种大小写形式），由 gen.sh 换成目标模块名。
//   两类都会把注释改成语义错误的内容（曾生成出「占位符 <项目名> 由 init.sh
//   替换成项目名」这种句子），第 2 类还会让注释指向一个不存在的目录。
//   故上面只写「路径段是模块名占位符」，不写出它本身。
import { ListExamples } from '../../../bindings/my-notice/app.js'
import { usePagedList } from '../../composables/usePagedList'

// TODO: 业务逻辑 —— 表单字段、校验、编辑/删除交互。
// 本页是黄金范例：演示管道契约的正确用法（分页 + 错误归一化）。
// 表格/表单等 UI 属产品面，由下游自行实现，此处的 <ul> 仅示意数据形状。

// 分页状态与加载收敛在 usePagedList 里：页大小上下界、错误归一化、
// 「以 Go 回显的归一化值为准」等契约随之自动生效，无需逐页重写样板。
const { list, total, page, pageSize, loading, error, load } = usePagedList(
  (req) => ListExamples(req),
  { label: 'ListExamples' }, // 慢调用告警里用它定位，见 lib/invoke.ts
)

onMounted(load)
</script>

<template>
  <section class="page">
    <h1 class="page__title">Example 列表</h1>

    <!-- 错误呈现：据 code 分流（此处仅示意，toast 等 UI 由下游实现） -->
    <p v-if="error" class="page__error">[{{ error.code }}] {{ error.message }}</p>

    <p v-if="loading">加载中…</p>
    <p v-else-if="!list.length" class="page__desc">暂无数据</p>
    <ul v-else>
      <li v-for="item in list" :key="item.id">{{ item.id }}</li>
    </ul>

    <!-- 分页信息：total 来自分页协议，前端据此渲染分页控件 -->
    <p class="page__total">共 {{ total }} 条，第 {{ page }} 页（每页 {{ pageSize }} 条）</p>
  </section>
</template>

<style scoped>
.page__title {
  font-size: var(--font-size-xl);
  margin: 0 0 var(--space-4);
}
.page__desc {
  color: var(--text-2);
}
.page__error {
  color: var(--color-danger);
}
.page__total {
  color: var(--text-2);
  font-size: var(--font-size-sm);
}
</style>
