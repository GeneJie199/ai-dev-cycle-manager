# Security Policy

## 适用范围

ai-dev-cycle-manager 是一个**本地**工具：数据保存在本机 SQLite 文件，`devcycle serve` 只监听 loopback（`127.0.0.1`），并会硬性拒绝绑定非 loopback 地址。

## 使用注意事项

- 远程使用必须通过 SSH 隧道。API 能写入数据库、执行经用户提交的验证命令并启动本机 AI CLI，因此不提供无鉴权的远程监听开关。
- 数据库文件（默认 `./devcycle.db`）包含需求与任务内容，请按普通敏感文件对待。
- 前端不使用任何 CDN / 第三方脚本，静态资源全部通过 Go `embed` 内置。

## 报告漏洞

如果你发现安全问题，请**不要**直接开公开 Issue。请通过 GitHub 仓库的 Security 页面（Security Advisories）私下报告，或在 Issue 中仅说明“存在安全问题”并留下联系方式，由维护者跟进。

我们会在确认后尽快修复并随版本发布说明披露。
