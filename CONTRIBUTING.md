# Contributing

欢迎贡献！本仓库是 Go 后端 + CLI + 本地 Web 工作台（无 Node 依赖）。

## 环境要求

- Go 1.22+（go.mod 中标注的版本为准）
- 系统 `git`（`internal/git` 通过 CLI 调用）

## 常用命令

```bash
go build ./...     # 构建
go test ./...      # 全部测试
go vet ./...       # 静态检查
gofmt -l .         # 应无输出（提交前请 gofmt -w .）
go run ./cmd/devcycle demo          # CLI 端到端演示
go run ./cmd/devcycle serve         # 启动本地 Web 工作台
```

CI 会执行 `gofmt`、`go vet`、`go test` 与 `go build`，请在提交前本地跑一遍。

## 代码约定

- 分层：`internal/models`（领域模型）→ `internal/store`（SQLite 持久化）→ `internal/git`（git CLI 封装）→ `internal/app`（对外门面）→ `internal/web`（HTTP API + embed 前端）与 `cmd/devcycle`（CLI）。
- 新能力优先加在 `internal/app`，CLI 与 Web 都只是薄壳；前端模板/脚本放 `internal/web/static`，保持零外部依赖（不引 CDN）。
- 改动了 `internal/store` 的表结构时，在 `migrate()` 中用 `IF NOT EXISTS` 风格保持向后兼容。
- 新功能补测试：store/app 用临时 SQLite，git/web 相关测试用 `t.TempDir()` 建真实仓库，git 不存在时 `t.Skip`。
- 术语遵循 README：Branch = 独立工作版本，Worktree = 隔离开发目录。

## 提交与 PR

- commit message 用英文、祈使句，说明“为什么”而不只是“改了什么”。
- 一个 PR 只做一件事；不要顺手重构无关代码。
- Bug 报告请附：OS、`git --version`、复现命令、完整错误输出。
- 安全问题请走 [SECURITY.md](SECURITY.md) 的私下渠道，不要开公开 Issue。
