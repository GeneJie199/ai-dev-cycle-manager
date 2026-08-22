# DevCycle

DevCycle 是本地优先的 AI 开发生命周期工作台，把一句交付目标转成可编辑计划，再沿着验收标准、开发任务、Agent 会话、验证证据和发布候选形成一条可审计链路。

[![CI](https://github.com/GeneJie199/ai-dev-cycle-manager/actions/workflows/ci.yml/badge.svg)](https://github.com/GeneJie199/ai-dev-cycle-manager/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

## 能做什么

- 从自然语言创建需求，手工或借助 AI 生成完整交付计划。
- 在保存和拆分前编辑范围、假设、待确认问题、验收标准、测试、任务依赖、风险、回滚与候选说明。
- 接入 Kimi、Codex、Claude 本机规划 CLI，以及 OpenAI Compatible、Anthropic、Gemini、本地 OpenAI/Ollama 和自定义 OpenAI API。
- 使用 Codex、Claude Code、Gemini CLI、Kimi Code、OpenCode 或安全的自定义命令适配器执行任务。
- 为任务创建独立 Branch + Worktree，保存 Agent 日志、生命周期、任务依赖与跨 Agent 交接记录。
- 运行 build/test/lint/smoke 命令，保存退出码、受限输出和标准化 Evidence。
- 以最新证据重新计算验收和发布就绪度，不能只靠前端勾选通过。
- 用 Humanized Git 展示新增、修改、删除、重命名、影响面、风险原因、建议验证和完整原始 Diff。
- 导出开发报告与发布候选 JSON，交给 ReleaseGuard 做独立发布验证。

Web UI、Lucide 图标和 SQLite 驱动全部嵌入单个 Go 二进制，运行时不依赖 Node、CDN、Prometheus 或云端控制面。

## 快速开始

需要 Go 1.26.6+ 和 Git。Agent 与 AI 是可选增强项，没有安装任何 AI 工具时，手工计划、Git、验证、证据和报告仍可完整使用。

```bash
go test ./...
go build -trimpath -o devcycle ./cmd/devcycle
./devcycle serve --db ./devcycle.db --addr 127.0.0.1:8766
```

Windows 使用 `devcycle.exe`。打开 `http://127.0.0.1:8766/` 后：

1. 在左侧输入想交付的目标并创建需求。
2. 让可用模型生成计划，或直接手工建立；逐项审阅后保存并拆为验收标准和任务。
3. 在“任务执行”导入仓库、创建隔离工作区，启动 Agent 或手工推进任务；需要换 Agent 时创建持久交接。
4. 在“验证证据”运行命令并登记代码、提交、测试、截图、HTTP 响应、制品或人工确认。
5. 在“报告发布”复核证据覆盖、代码来源和阻塞项，生成开发报告与发布候选。

也可以运行不依赖已有仓库的 CLI 演示：

```bash
./devcycle demo
```

## 产品界面

| 页面 | 交付能力 |
|---|---|
| 工作台 · 意图与计划 | 自然语言需求、AI 发送前预览、完整可编辑计划与原子拆分 |
| 工作台 · 验收标准 | 标准编辑、证据关联、通过与撤回 |
| 工作台 · 任务执行 | 依赖阻塞、Worktree、Agent 会话、日志与任务交接 |
| 工作台 · 验证证据 | 后台验证、实时输出、停止/超时/恢复、八类证据 |
| 工作台 · 报告发布 | 就绪度、来源提交、开发报告和发布候选 |
| 活动 | 全局 Agent 会话与验证运行状态 |
| 代码 | 结构化 Diff、原始 Diff、影响分析和 AI 改动解释 |
| 设置 | AI 提供方、Agent 适配器与本地仓库 |

桌面与移动端使用同一套界面。移动端以“需求列表 → 单需求详情”导航，五个阶段在容器内横向滚动，不会造成页面级溢出。

## AI 与 Agent

AI 规划和代码解释始终先给出可审阅结果，不会自动写代码、合并分支或批准发布。API 提供方发送前会显示目标地址、模型、脱敏内容和明确的数据边界。

API Key 只保存到操作系统凭据库，或在运行时从指定环境变量读取；SQLite、日志和 HTTP 响应中不保存凭据值。带凭据的远端提供方强制 HTTPS，本机回环地址可以使用 HTTP，请求禁止跨地址重定向并限制响应大小。

Agent 命令以可执行文件和参数数组直接启动，不经过 Shell。自定义适配器必须且只能包含一个 `{{prompt}}` 参数占位符，不能注入环境变量或 Shell 片段。详细配置见 [docs/configuration.md](docs/configuration.md)。

## HTTP API

| 路径 | 能力 |
|---|---|
| `/api/requirements` | 需求 CRUD、详情、报告与发布候选 |
| `/api/requirements/{id}/plan` | 读取和保存带修订号的完整计划文档 |
| `/api/requirements/{id}/ai-plan` | 生成计划草稿 |
| `/api/requirements/{id}/ai-plan/apply` | 原子拆分计划为标准和任务 |
| `/api/requirements/{id}/criteria`、`/api/criteria/{id}` | 验收标准创建、编辑、证据通过、撤回与删除 |
| `/api/tasks`、`/api/tasks/{id}` | 任务、依赖与状态 |
| `/api/tasks/{id}/worktree` | 创建并关联 Branch + Worktree |
| `/api/tasks/{id}/handoffs`、`/api/handoffs/{id}/accept` | 创建与接收任务交接 |
| `/api/agent-adapters` | 内置与自定义 Agent 适配器 |
| `/api/agent-sessions` | 启动、查询、停止会话并读取日志 |
| `/api/ai/providers` | API/CLI 提供方、凭据状态与连接测试 |
| `/api/ai/request-preview` | AI 出站请求预览与脱敏结果 |
| `/api/requirements/{id}/runs` | 后台验证运行与停止 |
| `/api/requirements/{id}/evidence` | 八类 Evidence 的创建和查询 |
| `/api/git/*` | status、structured diff、raw diff、branches、log、impact、AI explain |

所有写入 body 使用 JSON；未知字段被拒绝，请求体上限为 1 MiB。规划协议见 [docs/planning-document.md](docs/planning-document.md)。

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

## 安全与运维

服务硬性拒绝非 loopback 地址，因为本地 API 可以启动用户确认过的命令和 Agent。远程工作站请保持 DevCycle 监听本机，再通过 SSH 隧道访问。

Linux/macOS 用户可运行：

```bash
sh ./scripts/install.sh ./devcycle
```

备份、恢复、异常退出恢复和 systemd 用户服务见 [docs/operations.md](docs/operations.md)。发布标签自动构建 Linux、macOS、Windows 二进制并生成 SHA-256 校验文件。

## 边界

- DevCycle 不替代代码编辑器或 coding agent，也不伪造 Agent 完成状态。
- 不自动合并 Git 分支，不自动批准验收，不直接上线。
- 当前是单用户、本机工作台，没有多人账户、RBAC 或云同步。
- 验证命令和 Agent 提示来自本地可信用户，应像脚本一样审核。

发布候选使用 `lifecycle-spec/release-candidate/v1`，ReleaseGuard 会重新校验 readiness 和证据覆盖。Apache-2.0；贡献流程见 [CONTRIBUTING.md](CONTRIBUTING.md)，安全问题见 [SECURITY.md](SECURITY.md)。
