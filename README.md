# DevCycle

本地优先的 AI 开发生命周期工作台，把需求、验收标准、任务、Git 隔离目录、Agent 会话和发布证据放在同一条可审计链路里。

[![CI](https://github.com/GeneJie199/ai-dev-cycle-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/GeneJie199/ai-dev-cycle-manager/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

## 可交付能力

- 导入和移除本地 Git 仓库登记，读取状态、分支、提交、Diff 和变更影响。
- 管理需求、验收标准和任务，支持修改、删除和状态流转。
- 为任务创建独立 Branch + Worktree，不污染主工作区。
- 在任务 Worktree 内真实启动 `codex`、`kimi` 或 `claude` CLI，会话日志和状态落入 SQLite。
- 运行确定性的 build/test/lint/smoke 命令，保存退出码、截断输出和 Evidence。
- 只有带通过证据的验收标准才可满足；候选就绪度会重新计算，不能只靠前端勾选。
- 导出发布候选和开发报告，交给 ReleaseGuard 继续验证。
- 自带响应式中文 Web 工作台；静态资源嵌入单个 Go 二进制，无 Node/CDN 运行依赖。

## 快速开始

需要 Go 1.26+ 和 Git。Agent 功能按需安装对应 CLI；没有 Agent CLI 时其余能力仍可使用。

```bash
go test ./...
go build -trimpath -o devcycle ./cmd/devcycle
./devcycle serve --db ./devcycle.db --addr 127.0.0.1:8766
```

打开 `http://127.0.0.1:8766/`，完整工作流是：

1. 在“仓库”导入一个本地 Git 仓库。
2. 创建需求和可验证的验收标准。
3. 创建任务，并为任务创建独立 Worktree。
4. 启动 Agent 会话或运行验证命令。
5. 在“验收”登记证据；在“报告”复核影响和证据覆盖。
6. 在“发布候选”下载 JSON，交给 ReleaseGuard。

也可以先运行无需已有仓库的 CLI 演示：

```bash
./devcycle demo
```

## Web 工作台

| 视图 | 用途 |
|---|---|
| 仓库 | 导入/移除本地 Git 仓库 |
| 需求 | 创建、修改、删除需求 |
| 验收 | 管理标准并登记人工或自动证据 |
| 任务 | 状态流转、Worktree、Codex/KIMI/Claude 会话与日志 |
| 验证 | 执行命令、查看运行和 Evidence |
| Git | 状态、分支、提交、Diff、用户/API/数据库/配置影响 |
| 报告 | 汇总候选、证据、验证、Agent 会话和任务影响 |
| 发布候选 | 检查 readiness 并导出协议 JSON |

服务硬性拒绝非 loopback 地址。远程工作站请保持 DevCycle 监听本机，并通过 SSH 隧道访问；命令执行与 Agent API 不提供无鉴权远程模式。

## Agent 会话

DevCycle 调用本机已安装的工具，不模拟完成状态：

```text
codex exec PROMPT
kimi -p PROMPT
claude -p PROMPT
```

Agent 必须关联到已有 Worktree。日志写入数据库同目录下的 `devcycle-sessions/`，文件权限为仅当前用户可读写。正常停止、服务退出和异常重启都会留下明确的 `stopped` / `interrupted` / `failed` 状态。

## CLI

```text
devcycle version
devcycle import|status|branches|log|diff --repo PATH
devcycle worktree list|add|remove ...
devcycle requirement create|list|export|report ...
devcycle evidence add|list ...
devcycle verify --req ID --name NAME --command COMMAND --cwd PATH
devcycle task create|link|list ...
devcycle serve [--db PATH] [--addr 127.0.0.1:8766]
```

默认数据库是 `./devcycle.db`，可用 `--db` 或 `DEVCYCLE_DB` 覆盖。

## HTTP API

| 路径 | 能力 |
|---|---|
| `/api/repositories` | 仓库列表、导入、删除登记 |
| `/api/requirements` | 需求 CRUD、详情、开发报告、发布候选 |
| `/api/requirements/{id}/criteria` | 验收标准列表与创建 |
| `/api/criteria/{id}` | 验收结果/证据更新与删除 |
| `/api/tasks` | 任务列表、创建、状态更新和删除 |
| `/api/tasks/{id}/worktree` | 创建并关联 Branch + Worktree |
| `/api/agent-sessions` | 启动、列表、停止真实 Agent 会话并读取日志 |
| `/api/requirements/{id}/runs` | 执行和查询验证运行 |
| `/api/requirements/{id}/evidence` | 创建和查询 Evidence |
| `/api/git/*` | status、diff、branches、log、impact |

所有写入 body 使用 JSON；未知字段被拒绝。接口返回安全响应头，最大请求体为 1 MiB。

## 安装与运维

Linux/macOS 用户安装：

```bash
sh ./scripts/install.sh ./devcycle
```

systemd 用户服务、备份、恢复和安全边界见 [docs/operations.md](docs/operations.md)。发布标签自动构建 Linux、macOS、Windows 二进制并生成 SHA-256 校验文件。

## 边界

- DevCycle 不是代码编辑器，不替代 Codex/KIMI/Claude。
- 不自动合并 Git 分支，不自动批准验收，也不直接上线。
- 当前是单用户本地工作台；没有多人账户、RBAC 或云同步。
- 验证命令和 Agent 提示都来自本地可信用户，应像脚本一样审核。

## 协作

发布候选使用 `lifecycle-spec/release-candidate/v1`，ReleaseGuard 会重新校验 readiness 和证据覆盖，不能把 DevCycle 的布尔结果当作盲目信任。

Apache-2.0。贡献流程见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全问题见 [SECURITY.md](SECURITY.md)。
