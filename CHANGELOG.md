# Changelog

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
