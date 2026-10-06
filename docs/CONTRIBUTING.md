# 参与贡献

[README](../README.md) · [开发指南](development/README.md) · [English](#english)

## 报告问题

请提供软件版本、操作系统与架构、复现步骤，以及实际结果和预期结果。附上能说明问题的错误信息或截图，分享前按 [安全与隐私说明](SECURITY.md) 检查内容。

## 修改代码

按 README 启动开发工作区，使用 `.local/user-data/` 测试。Go 业务位于 `internal/`，桌面服务位于根目录，Vue 界面位于 `frontend/`。

- 修改 Go 服务或绑定后，运行 `npm run generate`。
- 修改界面时，遵循 [前端与界面约定](development/frontend.md#布局与导航)。
- 更新功能或命令说明时，同步中文和英文文档。
- 新增依赖或资源时，记录来源并保留许可证。

## 验证与提交

```sh
npm run check
```

只改文档时，检查链接、图片、命令和双语内容即可。功能修改应运行相关测试；涉及权限、播放器或原生能力时，说明实机验证范围。

PR 请写清要解决的问题、修改后的行为及验证结果。原创代码贡献应采用项目的 [GPL-3.0 许可证](../LICENSE)；引入第三方代码时，保留其来源与原有许可声明。

---

## English

[README](README.en.md) · [Development guide](development/README.en.md)

### Report a problem

Include the application version, OS and architecture, reproduction steps, and expected and actual results. Add relevant errors or screenshots after reviewing the [security and privacy notes](SECURITY.md#english).

### Change code

Launch the development workspace as described in the README and test with `.local/user-data/`. Go business code belongs in `internal/`, desktop services at the root, and the Vue interface in `frontend/`.

- Run `npm run generate` after changing Go services or bindings.
- Follow the [UI conventions](development/frontend.md#%E5%B8%83%E5%B1%80%E4%B8%8E%E5%AF%BC%E8%88%AA) for UI changes.
- Keep Chinese and English documentation in sync when changing features or commands.
- Record sources and retain licenses for new dependencies or assets.

### Verify and submit

Run `npm run check` for code changes. For documentation, check links, images, commands, and both language versions. State the extent of native testing when permissions, player control, or other native capabilities are affected.

Describe the problem, resulting behavior, and verification in the PR. Original code contributions should use the project's [GPL-3.0 license](../LICENSE). Retain the source and original license notices for third-party code.
