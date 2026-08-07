# ai-dev-cycle-manager

**内部代号：** DevCycle  
**一句话定位：** 把「需求 → 验收 → 任务 → Git 改动」留在本地可追踪，方便人和 AI 协作开发。

**专业名称：** AI Development Cycle Manager（后端）  
**通俗解释：** 不管具体用哪个 AI 编程工具，先把要做什么、怎么算做完、改动落在哪条分支/哪个目录管清楚。

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

属于 [AI DevOps Open Suite](https://github.com/GeneJie199/project-docs)。**本仓库当前是 Go 后端 + CLI**；Wails + React 桌面界面由前端单独推进，可绑定 `internal/app.App`。

---

## 为什么需要它

- AI 改代码很快，但需求、验收和 Diff 常常散落在聊天里；  
- 非专业同学面对 Branch / Commit / Worktree 容易劝退；  
- 团队需要「做完了」对应得上证据，而不是只看到模型说完成了。

本工具**不替代** Cursor / Codex / Claude Code，也不内置编辑器；它管理过程与结果的关联。

---

## 当前能做什么

| 能力 | 通俗说明 |
|------|----------|
| 导入本地 Git 仓库 | 确认路径是有效仓库 |
| 读状态 / 分支 / 提交 | 看现在改到哪了 |
| 需求与验收标准 | 记下要做什么、怎样算通过 |
| 任务 | 把需求拆成可执行条目 |
| 分支与 Worktree | 为任务创建「独立工作版本」和「隔离开发目录」 |
| Diff | 查看改动摘要 |
| Codex 适配器 | **仅接口定义**，尚未内置真实拉起多 Agent |

**不做（本阶段）：** 完整看板、云账号、自动合并/发布、多人权限、多 AI Provider 实现。

---

## 快速开始

需要：Go 1.22+、系统 `git`。

```bash
git clone https://github.com/GeneJie199/ai-dev-cycle-manager.git
cd ai-dev-cycle-manager
go test ./...
go run ./cmd/devcycle demo
```

指定已有仓库：

```bash
go run ./cmd/devcycle demo --repo /path/to/your/repo
```

常用命令见下方；默认数据库文件 `./devcycle.db`（可用 `--db` 或环境变量 `DEVCYCLE_DB`）。

```text
devcycle import|status|branches|log|diff --repo PATH
devcycle worktree list|add|remove ...
devcycle requirement create|list ...
devcycle task create|link|list ...
```

---

## 关键术语

| 专业名称 | 通俗解释 |
|----------|----------|
| Branch | 独立工作版本 |
| Worktree | 隔离开发目录 |
| Requirement | 需求：要解决的问题 |
| AcceptanceCriterion | 验收标准：怎样算做完 |
| Task | 任务：可分配、可挂分支的工作项 |
| Evidence（协议概念） | 证据：测试结果、确认记录等（跨工具协议已定义） |

---

## 给前端（Wails）的绑定入口

优先绑定 `internal/app.App` 上的方法，例如：`ImportRepository`、`GitStatus`、`AddWorktree`、`CreateRequirement`、`CreateTask`、`LinkTaskToWorktree` 等。详见源码。

---

## 贡献与许可

- Bug：请提供 OS、`git --version`、复现命令  
- Apache-2.0 — [LICENSE](LICENSE)  
- 套件总览：[project-docs](https://github.com/GeneJie199/project-docs)
