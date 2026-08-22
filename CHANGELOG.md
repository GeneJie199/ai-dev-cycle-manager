# Changelog

## 0.4.0 - 2026-08-22

- Add the editable `devcycle.planning-document/v1` contract with scope, assumptions, questions, criteria, test cases, strategy, ordered tasks, dependencies, risks, rollback concerns, candidate notes, durable revisions, and atomic application.
- Add configurable OpenAI-compatible, Anthropic, Gemini, local OpenAI/Ollama, and custom OpenAI providers alongside existing local CLI planning, with request previews, redaction, connection tests, bounded responses, HTTPS enforcement, and OS-keyring or environment-backed credentials.
- Add Codex, Claude Code, Gemini CLI, Kimi Code, and OpenCode adapters plus safe custom executable/argv adapters, availability checks, durable task handoffs, acceptance, changed-file context, and secret-safe logs.
- Expand Evidence to canonical code, commit, test, screenshot, command, HTTP response, artifact, and human-confirmation kinds while preserving legacy aliases.
- Replace the legacy workbench with a responsive five-stage delivery flow, global activity view, complete provider/adapter settings, offline Lucide icons, AI request confirmation, and polished empty/loading/error/danger states.
- Expand Humanized Git with added/modified/deleted/renamed classification, user/API/database/configuration/security impact, risk reasons, suggested verification, structured files, and a real raw patch view.
- Add schema-v3 migration coverage, revision-conflict tests, protocol fixtures, adapter/handoff tests, redaction tests, evidence ordering consistency, static product-surface checks, and desktop/mobile browser verification.

## 0.3.2 - 2026-08-13

- Add an actionable first-run workbench that opens and focuses the first requirement form instead of leaving a passive empty canvas.
- Add consistent visual product marks to the suite switcher without introducing external assets or runtime dependencies.
- Preserve expanded run/evidence details and manual log scroll position during live polling, and separate current per-criterion evidence from historical failures in readiness.

## 0.3.1 - 2026-08-12

- Align the suite patch release with the FleetScope transient CPU health-classification fix validated by the cross-module published-artifact gate.

## 0.3.0 - 2026-08-12

- Run verification commands as persisted background jobs with live bounded output, elapsed time, cancellation, terminal evidence, and restart recovery.
- Add reviewable AI planning and Git change explanation through installed Codex, KIMI, or Claude CLIs without silently applying suggestions.
- Pin release candidates to clean task worktrees and source commits, and expose structured development reports for ReleaseGuard.
- Add a shared suite header, configurable module switcher, contextual ReleaseGuard handoff, responsive mobile controls, and confirmation for process termination.
- Hard-reject non-loopback Web listeners because the local API can execute reviewed commands and Agent processes.
- Gate tag releases on Windows, macOS, and Linux tests plus race, static, vulnerability, formatting, vet, and changelog checks.

## 0.2.0 - 2026-08-12

- Deliver the local requirement, acceptance, task, evidence, report, and release-candidate workflow.
- Add real Git status, branch, log, diff, impact analysis, branch, and worktree integration.
- Add Codex, KIMI, and Claude CLI sessions with persisted lifecycle and logs.
- Require passing evidence before acceptance criteria can become release-ready.
- Add SQLite integrity rules, safe cascade behavior, responsive workbench views, and release-focused navigation.
- Add CI, multi-platform releases, checksums, install assets, operations guidance, and security defaults.
