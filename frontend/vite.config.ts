import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  server: {
    // 【必须显式绑 IPv4 回环，不能依赖默认值】
    //
    // Vite 的 server.host 默认是 'localhost'。Node 在 macOS 上把 localhost 解析成 ::1，
    // dev server 于是只监听 [::1]:<port>——IPv4 上没人听（lsof 实测确认）。
    // 而 wails3 的 dev 代理在 internal/assetserver/build_dev.go 里硬编码强制 IPv4：
    //     dialer.DialContext(ctx, "tcp4", addr)      // 目标是 127.0.0.1:<port>
    // 两者协议族对不上，代理每次都 connection refused，回给 webview 一个 502 ——
    // 页面纯空白。
    //
    // 为什么难查：`wails3 dev` 退出码 0，终端还照常打印
    // "Connected to frontend dev server!"。那句健康检查走的是 Go 默认 client（会先试
    // ::1 且成功），所以它【确实通了】，只是代理那条路不通。唯一线索是 app.log 里一行
    // 502，而它写的是 localhost，看不出是协议族的问题。
    //
    // 上游模板（internal/templates/**、examples/**）同样没设 host，属于
    // v3.0.0-beta.23 + Vite 7 的缺口，不是本仓配错。
    // 这条契约由 internal/guard/wiring_test.go 的护栏 15 兜住（漏了没有任何症状）。
    host: '127.0.0.1',
  },
})
