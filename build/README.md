# Build Directory

The build directory is used to house all the build files and assets for your application. 

The structure is:

* bin - Output directory
* darwin - macOS specific files
* windows - Windows specific files

## Mac

The `darwin` directory holds files specific to Mac builds.
These may be customised and used as part of the build. To return these files to the default state, simply delete them
and
build with `wails build`.

The directory contains the following files:

- `Info.plist` - the main plist file used for Mac builds. It is used when building using `wails build`.
- `Info.dev.plist` - same as the main plist file but used when building using `wails dev`.

## Windows

The `windows` directory contains the manifest and rc files used when building with `wails build`.
These may be customised for your application. To return these files to the default state, simply delete them and
build with `wails build`.

- `icon.ico` - The icon used for the application. This is used when building using `wails build`. If you wish to
  use a different icon, simply replace this file with your own. If it is missing, a new `icon.ico` file
  will be created using the `appicon.png` file in the build directory.
- `installer/*` - The files used to create the Windows installer. These are used when building using `wails build`.
- `info.json` - Application details used for Windows builds. The data here will be used by the Windows installer,
  as well as the application itself (right click the exe -> properties -> details)
- `wails.exe.manifest` - The main application manifest file.

## 应用图标

- **矢量源**：`build/icon/appicon.svg`（1024 画板；Apple 图标模板：824 内容区 + 圆角 185）。
  改图标只改这里，不要手改位图。
- **交付预览**：`build/icon/preview.html`——浏览器打开可看 16/24/32/48/64/128px 尺寸阶梯
  （深/浅两种底色），并按尺寸导出 PNG。
- **位图链路**：`build/appicon.png` 是唯一位图真相（同时被 `main.go` 嵌入作托盘图标），
  `darwin/icons.icns` 与 `windows/icon.ico` 由它派生：

  ```bash
  cd build && wails3 generate icons -input appicon.png \
    -macfilename darwin/icons.icns -windowsfilename windows/icon.ico
  ```

- **换图标流程**：改 SVG → 导出 1024 覆盖 `appicon.png` → 重跑上面的命令刷新 icns/ico。