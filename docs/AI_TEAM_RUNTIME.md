# AI Team runtime — development reference

Implementation: DevCycle `internal/team`, `internal/app/team.go` and
`internal/web/team.go`. Stable v1 workflows are retained. Start a locally built
DevCycle with `devcycle serve --db <path> --addr 127.0.0.1:8766`, then open `/team.html`.
The original workbench links to the new page.

Commands and custom adapter configuration must not embed credential literals.
Use CLI environment or keyring credentials. Variable references such as
`$OPENAI_API_KEY`, `$env:OPENAI_API_KEY`, `${OPENAI_API_KEY}` and `%OPENAI_API_KEY%`
are accepted as non-secret configuration text. Adapter argv is passed directly:
these references are expanded only by an explicitly configured shell or by a CLI
that supports them; DevCycle does not expand variables in argv. Verification
history stores redacted command/name text, while executing the original command.

## Execution contract

Mission owns a public project ID, requirement, acceptance criteria, plan/DAG, registry,
risk, budget, memory snapshot, policy and execution history. Roles are lead, frontend,
explorer, QA and review. Each task uses the existing DevCycle worktree APIs, a unique
branch and an existing managed Agent session. Context handoffs use the existing
durable handoff service. Source is inherited serially from the last completed task's
commit; this initial local scheduler does not run parallel implementation branches.

Expert may configure `worktreeRoot`, `branchPrefix`, context, budget, test commands and an independently modeled review policy.

`POST /api/team/missions` creates a validated mission. `POST .../{id}/advance` claims
and executes at most one ready task; clients can loop to completion/escalation. The
page does this automatically. GET endpoints never start execution. Repeated advances
are serialized per mission; revision CAS and an OS-held sidecar lock prevent duplicate
writers. `POST .../{id}/retry` cannot reset attempts, raise budgets or approve R4.

States: pending, blocked, ready, running, completed, failed, retry, escalated.
Failed execution records survive retries. Missing capabilities, exhausted budgets,
unsupported enforcement or ambiguous recovery fail closed. A process exit alone is
insufficient: source boundaries, committed clean state, persisted execution evidence,
machine checks and required independent structured review are verified.

Review findings can return work to lead under bounded `budget.maxFixes`; QA and review
then receive fresh worktrees from the repaired branch. No automatic main merge occurs.
Each plan task retains one logical DevCycle task ID across attempts. Every attempt
gets a new worktree and branch; only the current attempt is linked on that task.
Reopened work returns to todo/in-progress until it actually succeeds. Failed
executions, their exact artifact locations, sessions and evidence remain in history.
QA must depend on implementation and review must depend on QA; cycles/unknown
dependencies/duplicate IDs are rejected. R3 requires configured integration/security
checks and two successful review tasks. R4 is never automatically executed/approved.

## Git and permission boundaries

Explorer/review are checked for no source or HEAD change. Frontend can integrate only
`frontend/`, `web/`, `internal/web/static/`. QA can integrate tests, `*_test.go`, docs.
Lead owns implementation and integration decisions. Unsupported paths fail the task.
These checks contain integration changes; they are not OS isolation. A trusted CLI
retains its OS user's privileges. Explicit `trustedLocalCLI` consent is required;
requested enforced tool/network policies are rejected until a real enforcing executor
is implemented. Never use an untrusted CLI or source repository with ambient secrets.

An opt-in `policy.commitTaskChanges` lets the runtime validate the changed path set
and commit it through the existing Git client. This supports sandboxed CLIs whose
worktree index is write-protected. It does not bypass the CLI sandbox, merge main,
rewrite history or push automatically. Without this policy, an agent must commit its
own allowed changes. Read-only roles can never use it to commit edits.

## Persistence, recovery and evidence

Team state lives in `<original-db>.team.db` (schema 1), opened lazily. The original
DevCycle DB remains schema 3; v1 binary rollback does not need to read team state.
Back up the original DB, team sidecar, worktrees and session/verification logs together
while stopped. OS locks release on crash. Startup recovery interrupts existing managed
sessions; advance records ambiguous running team execution as failed and never assumes
it succeeded or silently replays its side effects.

Evidence binds mission/execution/session/provider/model, duration, retry/escalation,
test/review outcome, source branch/HEAD and SHA-256 of the bounded redacted log.
All final task heads/clean states are rechecked before completion. Acceptance refers
to configured command/review scope; it does not claim untested product semantics.
The standard DevCycle development report and release candidate remain available.

Review adapters use a channel contract: stdout contains the final structured review
response; prompt echoes, tool/source samples and diagnostics belong on stderr. Both
streams remain in the bounded redacted audit log. A separate redacted stdout file
is capped at 1 MiB and carries a completion, truncation and SHA-256 integrity record.
Review reads only this complete response; missing, incomplete, truncated or changed
responses fail closed, including sessions from older versions without response files.
Conflicting stdout verdicts and nonempty findings still fail; the runtime never
selects only the last JSON line. Execution evidence retains the audit log hash and
adds the response hash and transport. Historical outputs and errors are embedded as
single-line JSON data so their line breaks cannot masquerade as current verdicts.

Synchronous and asynchronous machine commands record runtime-owned string metadata:
`verificationRunId`, `sourceRepository`, `sourceWorktree`, `sourceCommit`,
`sourceClean`, and the corresponding `sourceRepositoryAfter`, `sourceWorktreeAfter`,
`sourceCommitAfter`, `sourceCleanAfter`. `sourceVerified=true` requires the same
Git repository, worktree and HEAD, with clean state both before and after execution.
Source changes make verification fail even if the process exit code is zero; the
actual exit code remains recorded. A non-Git command can run but records
`sourceVerified=false` and cannot provide source-bound release proof. Public evidence
creation cannot supply these reserved attestation fields.

When a candidate has machine runs or belongs to AI Team, readiness checks the latest
proof for each command, working directory and task against the exported sources.
Controller-owned QA proof additionally carries `missionId`, `planTaskId`,
`executionId`, `verificationAttempt`, `verificationCheckIndex` and `logicalCheckId`.
The logical check ID is SHA-256 of mission ID, plan task ID, check ordinal and the
original command, separated by NUL bytes. Only this reserved controller identity
permits a higher attempt of the same check to supersede older evidence across
worktrees. Different checks or tasks cannot hide one another's failures. Public
evidence creation cannot supply these reserved control fields. Generic verification
without this identity keeps the strict command/directory/task grouping.
Each source HEAD must be covered by a verified machine commit or its ancestor chain;
an explorer's older baseline cannot cover a newer untested implementation. A clean
new commit makes old proof stale until the check is rerun. A fresh same-command pass
can replace earlier failed or stale proof. Legacy manual candidates without machine
runs retain their existing readiness behavior. These additions use existing evidence
metadata and `readiness.ready`; neither schema 3 nor the v1 candidate DTO changes.

## API

| Endpoint | Purpose |
|---|---|
| `/api/team/missions` | GET list; POST create Simple/Expert configuration |
| `/api/team/missions/{id}` | GET durable plan, executions, policies and evidence IDs |
| `/api/team/missions/{id}/advance` | POST one dispatch/recovery step |
| `/api/team/missions/{id}/retry` | POST bounded retry of eligible escalated tasks |
| `/api/team/memory/{project}` | GET current memory or `?history=true`; PUT revisioned edit |
| `/api/team/evaluations` | GET task/agent/model/result/test/review/usage history |
| `/api/team/projects/{project}/status` | GET shared project/navigation contract foundation |
| `/api/team/about`, `/api/team/license` | GET build/source identity and full license |

Mutation endpoints reject cross-origin browser requests. Services remain loopback-only.
No telemetry, provider request, deployment or background scheduler is started by GET.
Public extension API: `api/extensions/v2` (capability, entitlement, policy, deployment
plan interfaces). Community has no dependency on Enterprise or a license server.
