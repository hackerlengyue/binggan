# 前端开发与界面约定

[文档导航](../README.md) · [开发指南](README.md) · [English](#english)

Vue 界面随 MyGo 桌面应用加载，包含密钥提取、密钥管理、播放编排、视频解密、资源管理及设置页面。组件使用 shadcn-vue，样式使用 Tailwind。

## 本地开发

从项目根目录运行 `npm run dev`，同时启动桌面窗口和 Vite。在 `frontend/` 中可单独执行：

```sh
npm ci
npm test
npm run build
```

单独运行 Vite 可查看界面；采集、权限、文件操作和播放编排需要桌面壳。

## 服务调用

`frontend/src/mygo.ts` 是生成的 Go 服务客户端，修改绑定后从根目录执行 `npm run generate`。业务调用使用生成绑定，实时采集使用 Channel，媒体使用自定义 Protocol。Vite 不代理业务 API。

## 组件来源

布局参考 shadcn-vue 的 [dashboard-01](https://shadcn-vue.com/blocks/dashboard#dashboard-01)，基础组件沿用 `frontend/components.json` 中的 `reka-nova` 配置。

组件源码保存在 `frontend/src/components/ui/`，公共样式保存在 `frontend/src/third-party/shadcn-vue.css`，来源版本为 `2.8.2`。日常安装与构建无需组件生成 CLI；[MIT 许可证](../../frontend/public/licenses/shadcn-vue-MIT.txt)随应用打包。

第三方来源与许可见 [第三方说明](../third-party/README.md)。

组件样式以 `frontend/src/style.css` 为准，页面范围以 `frontend/src/router.ts` 为准。

## 布局与导航

桌面使用 SidebarProvider、AppSidebar、SidebarInset 和 SiteHeader，左侧导航、右侧工作区。侧栏宽度为 `13rem`，顶栏高度为 `52px`。导航分为视频处理与系统管理，底部菜单提供外观和语言设置。

| 区域 | 页面 |
| --- | --- |
| 概览 | 数据概览。 |
| 视频处理 | 密钥提取、密钥管理、播放编排、视频解密、资源管理。 |
| 系统管理 | 证书管理、环境信息、解密设置、电源管理、通知管理、日志信息。 |

路由使用 hash 模式。

内容沿用 `px-4 lg:px-6`、`py-4 md:py-6`、`gap-4 md:gap-6`。表格按内容高度展示，工具栏和分页在表格之外。请求详情隐藏外层标签栏，保留返回与前后请求导航。

## 主题与字体

界面使用 Neutral 基础色和 Blue 主题。桌面密度、圆角、边框、阴影与语义色集中在 `frontend/src/style.css`。

| 变量 | 用途 |
| --- | --- |
| `--primary` / `--primary-foreground` | 主要操作与按钮文字。 |
| `--sidebar-primary` | 侧栏产品图标。 |
| `--background` / `--canvas` | 工作区和外层背景。 |
| `--muted` | 次要表面。 |
| `--success` / `--warning` / `--info` | 业务状态。 |
| `--radius` | 基础圆角，当前为 `0.5rem`。 |

主要操作使用默认按钮，次要操作使用 outline 或 ghost，危险操作使用 destructive。状态色区分进行中、完成、失败、部分失败和排队；深色主题使用对应深底色。

运行时使用系统字体栈，字体配置见 `frontend/src/style.css`。`frontend/components.json` 中的 Inter 记录初始预设，运行时不加载远程字体。密码和代码使用等宽字体，计数使用 `tabular-nums`，中文保持正常字间距。

## 基础组件

沿用 shadcn-vue 的 Button、Card、Table、Tabs、Input、Dialog 等组件。命名使用 Dialog，删除确认使用 AlertDialog，空状态使用 Empty 或 TableEmpty，分页使用 Pagination，表单使用 Field 和 Input，开关使用 Toggle 或 ToggleGroup。Badge 的 success、warning、info 变体表示业务状态。

新增页面复用现有主题变量、组件反馈和间距，不另加独立色板、装饰图表或重复阴影。图表应对应真实业务数据。概览使用官方 Chart 组件展示近 14 天趋势、最近 6 次密码数量和记录完整度，统计排除当前进行中的采集。

## 采集与历史记录

一次手动采集分配独立 UUID，界面选择不改变数据归属。开始新采集前保存上一条记录并清空工作缓冲。停止后先等待已接收请求写入，再保存响应和密钥，最后弹出命名对话框。

命名时保留页面内容，保存成功或使用默认名称后清空提取页；失败可原地重试，不能清空未保存内容。没有提取到密码的请求也会保存为独立记录，但空密钥记录不能导出无效 JSON。

历史详情仅使用所选记录的请求与密钥，不回退到当前采集。记录缺少原始请求时显示“未保存请求内容”。名称可修改，密码值保留采集原值；JSON 下载沿用 `passwords` / `getPwdData` 结构。

重复密码值去重，不同密码值不因相同请求序号而覆盖。删除只作用于选中记录，采集中的记录须先停止；写入失败保留原内容。删除标记防止刷新或其他标签页恢复已删除记录。

SQLite 保存密钥历史。桌面应用启动时从数据库加载记录，再更新 localStorage 缓存。

## 列表、任务与日志

- 列表保持单行，长名称可截断，但提供完整 title 或详情入口。数量列居中，时间列位于操作之前，图标操作附名称提示。
- 日期时间显示为本地 `YYYY-MM-DD HH:MM:SS`，存储值、记录名称和下载原文保持原样。
- 新建解密任务先选视频，再逐项确认密钥。按钮与回车使用相同校验；选密钥子视图中按回车不提交任务。
- 日志支持搜索、重置筛选和完整下载；无效时间范围禁止导出。
- 请求详情中的 JSON 使用文本节点高亮，保留原始正文与复制功能；不另建 JSON 预览页或原始抓包导出入口。

## 窄屏适配与验证

窄屏侧栏收起为 Sheet，选择导航后关闭。操作按钮和文件信息可分行，正文自动换行，表格在容器内横向滚动；顶栏保持可见。

界面修改需检查深浅色主题、桌面和 `375px` / `320px` 窄屏。使用隔离数据验证命名、下载、原始请求归属、刷新持久化、删除和请求缺失时的显示，并运行前端测试与构建。

---

## English

[Project README](../README.en.md) · [Development guide](README.en.md)

The Vue interface runs inside the MyGo desktop application. It includes key capture and management, playback orchestration, video decryption, media management, and settings. Components use shadcn-vue; styling uses Tailwind.

### Local development

Run `npm run dev` from the project root to start the native window and Vite. Inside `frontend/`, use `npm ci`, `npm test`, and `npm run build` for frontend installation, tests, and production builds.

Vite alone can render the interface. Capture, permissions, file operations, and playback orchestration need the desktop shell.

### Service calls and UI conventions

Regenerate `frontend/src/mygo.ts` with the root `npm run generate` command after binding changes. Service calls use generated bindings, live capture uses Channels, and media uses a custom Protocol. Vite does not proxy business APIs.

The layout draws on shadcn-vue's [dashboard-01](https://shadcn-vue.com/blocks/dashboard#dashboard-01). Base components use the `reka-nova` configuration in `frontend/components.json`.

Component source lives in `frontend/src/components/ui/`; shared styles from version `2.8.2` live in `frontend/src/third-party/shadcn-vue.css`. Installation and builds do not require the component generator CLI. Its [MIT license](../../frontend/public/licenses/shadcn-vue-MIT.txt) is included in the application bundle.

The sections above cover [layout and navigation](#%E5%B8%83%E5%B1%80%E4%B8%8E%E5%AF%BC%E8%88%AA), [themes and fonts](#%E4%B8%BB%E9%A2%98%E4%B8%8E%E5%AD%97%E4%BD%93), and [capture and history](#%E9%87%87%E9%9B%86%E4%B8%8E%E5%8E%86%E5%8F%B2%E8%AE%B0%E5%BD%95). See [third-party notices](../third-party/README.md#english) for sources and licenses.
