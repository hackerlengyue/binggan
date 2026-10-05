# 第三方许可与资源

[文档导航](../README.md) · [项目首页](../../README.md) · [English](#english)

各依赖和资源按其原许可证使用。本页列出主要来源与许可文本；完整依赖版本以 `go.mod`、`go.sum` 和两份 npm 锁文件为准。

## 来源与许可

| 资源 | 来源 | 许可或本地说明 |
| --- | --- | --- |
| MyGo | [egoist/mygo](https://github.com/egoist/mygo) | [MIT](../../resources/licenses/MyGo-MIT.txt) |
| sing-box | [SagerNet/sing-box](https://github.com/SagerNet/sing-box) | [GPL-3.0](../../resources/licenses/sing-box-GPL-3.0.txt) |
| sing-tun | [SagerNet/sing-tun](https://github.com/SagerNet/sing-tun) | [GPL-3.0](../../resources/licenses/sing-tun-GPL-3.0.txt) |
| FFmpeg / ffprobe | `resources/FFMPEG-SOURCES-*.json` | [平台说明](ffmpeg/) 与各平台 `resources/<platform>/licenses/ffmpeg/` 中的许可文件。 |
| PermissionFlow 2.11.2 | [jaywcjlove/PermissionFlow](https://github.com/jaywcjlove/PermissionFlow) | [MIT](../../resources/darwin/licenses/PermissionFlow-MIT.txt)，版本记录在 `native/permission-helper/Package.resolved`。 |
| shadcn-vue | [unovue/shadcn-vue](https://github.com/unovue/shadcn-vue) | [MIT](../../frontend/public/licenses/shadcn-vue-MIT.txt) 与 [组件、布局及样式来源](../development/frontend.md#组件来源)。 |
| Apple TV Like Player | [doraFX/apple-tv-like-player](https://github.com/doraFX/apple-tv-like-player) | [NOTICE](apple-tv-like-player.md) 与 [MIT](../../frontend/public/player/LICENSE)。 |
| LXGW WenKai v1.522 | [项目发布页](https://github.com/lxgw/LxgwWenKai/releases/tag/v1.522) | [字体来源](lxgw-wenkai.md) 与 [SIL OFL 1.1](../../frontend/public/fonts/lxgw-wenkai/OFL.txt)。 |

`resources/LICENSE` 属于 sing-box 相关许可说明。项目主许可证尚未确定；第三方许可文本不替代项目主许可证。分发平台工具时，需同时保留对应的许可证与来源信息。

## Logo、图标与视频

这些素材由项目提供，尚未声明单独的复用许可。`docs/assets/` 中的图片为软件界面截图。

## 测试夹具

`internal/engine/testdata/` 中的小型媒体与密钥算法 JSON 用于测试。JSON 内容是固定重复字符、示例密码和参考向量，不是用户采集结果。

---

## English

Each dependency and asset retains its original license. The table above links the main sources and local notices. The complete version inventory is in `go.mod`, `go.sum`, and both npm lockfiles.

`resources/LICENSE` is a sing-box-related notice. The main project license is undecided; dependency licenses do not establish a license for the whole project. Keep the relevant licenses and source information with distributed platform tools.

The project supplies the logo, icons, and introduction video without a separate reuse license declared here. Images in `docs/assets/` are screenshots of the application.

Small media and key-algorithm fixtures in `internal/engine/testdata/` use fixed repeated characters, example passwords, and reference vectors. They are test inputs rather than captured user data.
