# ai-dev-cycle-manager

Go backend library + CLI for a local **AI-assisted development cycle** manager.

> **UI note:** A **Wails + React** desktop frontend is planned and will be built separately.  
> This repository currently ships the **Go library**, **SQLite store**, **git CLI wrappers**, and a **`devcycle` CLI** for demos and e2e checks. APIs on `internal/app.App` are shaped for later Wails method binding.

**License:** [Apache-2.0](LICENSE)

## Terminology

| English   | Meaning (docs)   | Role                                      |
|-----------|------------------|-------------------------------------------|
| Branch    | 独立工作版本     | Named git branch for a task               |
| Worktree  | 隔离开发目录     | Separate checkout directory via `git worktree` |

## Features (DEV-001 … DEV-005)

- **DEV-001** — Go module scaffold: `cmd/devcycle`, `internal/*`, SQLite via [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) (pure Go, Windows-friendly)
- **DEV-002** — Import / validate a local git repository (system `git` CLI)
- **DEV-003** — Read `status`, branches, commit log
- **DEV-004** — Worktree create / list / remove
- **DEV-005** — SQLite models + CRUD for `Requirement`, `AcceptanceCriterion`, `Task` (tasks link to branch + worktree path)

Also included:

- **Codex CLI adapter interface only** (`internal/agent/codex.go`) — `Start` / `Stop` / `Status`; no fake “completed” runner
- **Git diff reader** — `git diff` / `git diff --stat`
- **E2E demo** — `devcycle demo` or `go run ./examples/demo`

## Requirements

- Go 1.22+ (developed with Go 1.26)
- System `git` on `PATH`

## Quick start

```bash
git clone https://github.com/GeneJie199/ai-dev-cycle-manager.git
cd ai-dev-cycle-manager
go test ./...
go run ./cmd/devcycle demo
```

Or build the CLI:

```bash
go build -o devcycle.exe ./cmd/devcycle   # Windows
./devcycle demo
./devcycle demo --repo C:\path\to\existing\repo
```

## CLI usage

```text
devcycle demo [--repo PATH] [--db PATH]
devcycle import --repo PATH [--db PATH]
devcycle status --repo PATH
devcycle branches --repo PATH [--remote]
devcycle log --repo PATH [-n N]
devcycle diff --repo PATH [--stat] [--staged]
devcycle worktree list --repo PATH
devcycle worktree add --repo PATH --path WT --branch NAME --create-branch
devcycle worktree remove --repo PATH --path WT [--force]
devcycle requirement create --title T [--desc D] [--db PATH]
devcycle requirement list [--db PATH]
devcycle task create --req ID --title T [--db PATH]
devcycle task link --repo PATH --task ID --branch B --path WT [--db PATH]
devcycle task list [--db PATH]
```

Default SQLite file: `./devcycle.db` (override with `--db` or `DEVCYCLE_DB`).

### Demo flow

`devcycle demo` will:

1. Init a temp git repo **or** use `--repo`
2. Import / validate the repo
3. Create a requirement, acceptance criterion, and task
4. Create a branch (独立工作版本) + worktree (隔离开发目录) and link them on the task
5. Print status / log / worktree summary

## Layout

```text
cmd/devcycle/          CLI entrypoint
examples/demo/         `go run` wrapper for the demo
internal/app/          High-level façade (Wails-bindable methods)
internal/git/          git CLI wrappers (repo, status, worktree, diff)
internal/store/        SQLite persistence
internal/models/       Domain types
internal/agent/        CodexAdapter interface only
pkg/devcycle/          Shared type aliases for hosts
```

## Wails binding (for the frontend agent)

Bind methods on `internal/app.App` from the future Wails `main` package (same module). Useful exports:

| Method | Purpose |
|--------|---------|
| `ImportRepository` | Register local git path |
| `GitStatus` / `GitBranches` / `GitLog` / `GitDiff` | Repo introspection |
| `ListWorktrees` / `AddWorktree` / `RemoveWorktree` | Worktree ops |
| `CreateRequirement` / `CreateCriterion` / `CreateTask` | Domain CRUD |
| `LinkTaskToWorktree` | Branch + worktree + task link |
| `SetCodexAdapter` | Inject a real Codex CLI implementation later |

**Not in this repo (by design):** built-in editor, full kanban UI, cloud accounts, auto-merge/release, multi-user ACL, multiple AI providers, React/Wails UI.

## Tests

```bash
go test ./...
```

Git tests create a **real temporary repository** and invoke the system `git` binary.

## License

Copyright 2026 GeneJie199. Licensed under the Apache License, Version 2.0.
