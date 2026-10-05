# 开发与打包

[项目首页](../../README.md) · [文档导航](../README.md) · [English](README.en.md)

## 开始开发

先按 [快速开始](../../README.md#快速开始) 安装依赖，并准备当前平台的工具。根目录负责 MyGo 桌面壳与 Go 服务，`frontend/` 负责 Vue 界面。开发入口会同时启动原生窗口和 Vite。

```sh
npm run dev
```

根 npm 项目、前端 npm 项目和 Go module 各自使用锁文件。MyGo CLI 固定为 `0.2.5`。

## 常用命令

以下命令在项目根目录执行：

| 命令 | 用途 |
| --- | --- |
| `npm ci` | 安装 MyGo CLI。 |
| `npm run setup` | 安装前端依赖、下载 Go module、生成绑定。 |
| `npm run dev` | 启动开发应用。 |
| `npm run generate` | 根据 Go 服务重新生成前端客户端。 |
| `npm test` | 运行 Go 与前端测试。 |
| `npm --prefix frontend run build` | 前端类型检查与生产构建。 |
| `npm run check` | 生成绑定、运行测试、构建前端并检查项目结构与文件。 |
| `npm run check:repository` | 单独检查仓库文件。 |

## 服务与界面通信

Go 服务通过 MyGo 生成客户端供前端调用。修改服务方法或绑定后，运行 `npm run generate` 更新 `frontend/src/mygo.ts`。

普通业务使用生成绑定，实时采集使用 Channel，媒体和附件通过 `binggan-stream` Protocol 提供。Vite 不代理业务 API。前端组件与布局约定见 [前端开发](frontend.md)。

## 数据目录

### 开发与打包应用

| 配置 | 开发默认值 | 打包应用默认值 |
| --- | --- | --- |
| 数据目录 | `.local/user-data/` | 系统应用数据目录下的 `饼干大小姐 V2` |
| 接收端口 | `18788` | `18778` |
| 代理端口 | `18789` | `18779` |
| Vite 地址 | `127.0.0.1:5175` | 使用打包后的前端 |

可用 `BINGGAN_USER_DATA_DIR`、`BINGGAN_RECEIVER_PORT`、`CAPTURE_PROXY_PORT` 覆盖数据路径和端口。

开发入口在数据目录内生成 `.workspace-id`，据此使用独立 Bundle ID，隔离 WebView 存储。保留数据目录时身份不变，换新目录时会生成新身份。打包应用 ID 为 `time.binggan.haomen.v2`。

要从空白状态开始，先退出应用，再把开发数据目录移到备份位置。下一次启动会创建新目录；指定 `BINGGAN_USER_DATA_DIR` 指向备份，可重新打开原工作区。

## 平台工具

```sh
npm run prepare:resources -- --platform darwin-arm64
npm run prepare:resources -- --verify --platform darwin-arm64
```

提供 `darwin-arm64`、`darwin-amd64`、`windows-amd64`、`windows-arm64` 和 `all` 目标，其中 Windows 未进行测试。脚本按 `resources/FFMPEG-SOURCES-*.json` 下载 FFmpeg、ffprobe 与许可文件，校验下载及提取文件的 SHA-256，检查可执行文件架构。Darwin 目标还会编译采集辅助程序。

缓存位于 `.cache/resources/`，工具位于 `resources/<platform>/tools/`。测试入口会将当前平台工具加入 PATH。直接运行 Go 测试时，也需确保 FFmpeg 和 ffprobe 可从 PATH 找到。

## 测试

```sh
npm run check
```

完整检查使用以下范围的 Go 测试：

```sh
go test . ./cmd/... ./internal/...
```

这能避免把前端依赖中自带的 Go 包扫入测试。绑定变更完成后，也应确认生成客户端没有遗漏。

### 可选原生测试

准备 macOS ARM64 工具并构建前端后，可运行真实 WKWebView 测试：

```sh
BINGGAN_MYGO_E2E=1 go test . -run '^TestMyGoWebView$' -count=1 -v
```

| 环境变量 | 用途 |
| --- | --- |
| `BINGGAN_MYGO_E2E=1` | 在临时工作区运行真实 WKWebView 测试。 |
| `BINGGAN_MAC_BUNDLE` | 核验指定应用包。 |
| `BINGGAN_TEST_INSTALLED_SZPLAYER=1` | 检查本机已安装的播放器。 |
| `BINGGAN_SURGE_INTEGRATION=1` | 执行原生分流集成测试并恢复配置。 |

普通测试使用隔离环境。系统授权、真实播放器完整队列、证书信任和长期防息屏仍需在实机检查；WebView 或模拟控件测试不能替代这些验证。Windows 未进行测试。

## 应用打包

### macOS

需要 macOS、Go、Xcode Command Line Tools、Swift `6.2+` 和兼容 SDK。开发入口与打包脚本会启用辅助功能驱动所需的 cgo。

```sh
# Apple Silicon
npm run prepare:resources -- --platform darwin-arm64
bash scripts/build-macos.sh arm64

# Intel
npm run prepare:resources -- --platform darwin-amd64
bash scripts/build-macos.sh amd64

# Universal
npm run prepare:resources -- --platform darwin-arm64
npm run prepare:resources -- --platform darwin-amd64
bash scripts/build-macos.sh universal
```

脚本编译 PermissionFlow 辅助程序和本地化资源，再检查架构、Bundle ID 与签名。应用输出为 `build/darwin-<target>/饼干大小姐.app`。

SDK 优先使用开发工具目录中已安装的 `26.5`，否则使用 `xcrun` 返回的当前 SDK；可通过 `BINGGAN_PERMISSION_SDK` 指定。SDK `26.5` 已用于验证这套打包流程。若出现 `SwiftUIMacros.StateMacro` 插件缺失，请检查 SDK 与开发工具是否配套。

当前脚本使用本地 ad-hoc 签名。公开分发时需按发布渠道配置 Developer ID、签名和公证。

### Windows（未进行测试）

Windows 的启动、WebView2、证书、采集与导出均未进行测试。GitHub Actions 提供 x64 与 ARM64 构建，以下为本地构建命令；播放编排的原生驱动目前仅在 macOS 实现。

```sh
npm run prepare:resources -- --platform windows-amd64
npm run prepare:resources -- --platform windows-arm64
npm run build -- -platform windows/amd64,windows/arm64
```

Windows 构建入口会为 MyGo CLI 的 NSIS 调用指定 UTF-8，支持中文应用名称和路径。兼容处理使用临时的 CLI 源码副本；Go module 缓存保持原样。升级 MyGo CLI 时需重新核对这项处理。

## 自动构建与发布

[构建流程](../../.github/workflows/ci.yml) 先在 macOS 上运行 Go 与前端测试、类型检查、生成绑定一致性及仓库检查，通过后再使用 MyGo 构建应用。

| 触发方式 | 结果 |
| --- | --- |
| 推送 `main` 或提交 PR | 执行检查，构建 macOS ARM64 / x64 和 Windows ARM64 / x64（运行未进行测试）。 |
| 在 Actions 中选择 Run workflow | 手动执行同一构建流程。 |
| 推送 `v` 开头的版本标签 | 完成构建后，将四个平台的压缩包与 SHA-256 校验文件上传至 GitHub Release。 |

在 [Build 构建记录](https://github.com/hackerlengyue/binggan/actions/workflows/ci.yml) 中打开一次成功的运行，即可下载页面下方的 Artifacts，产物保留 14 天。标签构建的产物也会出现在 [Releases](https://github.com/hackerlengyue/binggan/releases)。

标签必须与 `package.json` 中的版本一致，例如当前 `2.0.0` 对应 `v2.0.0`。更新版本时同步修改 `package.json`、锁文件、`frontend/package.json` 和 `mygo.config.ts`，提交后再推送标签：

```sh
git tag v2.0.0
git push origin v2.0.0
```

macOS 产物使用本地 ad-hoc 签名，包含辅助功能驱动、PermissionFlow 和各自许可证。Windows 的实际运行未进行测试。CI 不启用需要真实播放器、证书信任或系统网络环境的可选测试。
