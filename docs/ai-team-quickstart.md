# AI Team quickstart (development preview)

AI Team is the v2 development preview, not a GA release. It runs local coding CLIs through DevCycle and keeps task branches, checks and review evidence.

## Set up

Install Git, the Go toolchain required by [go.mod](../go.mod) (currently 1.26.10), and an authenticated coding CLI available to the DevCycle process. From the repository root:

```sh
go build -trimpath -o devcycle ./cmd/devcycle
./devcycle serve --db ./devcycle.db --addr 127.0.0.1:8766
```

On Windows, build with `-o devcycle.exe` and run `.\devcycle.exe`. Open <http://127.0.0.1:8766/team.html>.

In the existing workbench's **Settings**, configure and enable a custom Agent adapter. AI Team requires an explicit `{{model}}` argument placeholder; the current built-in adapters lack it. For example, use adapter ID `codex-team`, command `codex`, and arguments:

```json
["exec", "--model", "{{model}}", "--sandbox", "workspace-write", "{{prompt}}"]
```

Use your installed CLI's actual noninteractive syntax. Arguments execute directly without a shell. Include exactly one prompt placeholder: `{{prompt}}`, or `{{prompt_stdin}}` for a CLI that accepts `-` and reads its prompt from stdin.

## Start a mission

1. Enter a bounded goal, local Git repository path and acceptance commands (one per line).
2. Expand team settings and enter a capability registry. Replace the example model ID with an actual model your CLI can use, and set its real context limit:

   ```json
   [{
     "id": "YOUR_MODEL_ID",
     "provider": "openai",
     "adapter": "codex-team",
     "capabilities": ["exploration", "coding", "testing", "review"],
     "contextLimit": 128000,
     "tier": 2,
     "available": true,
     "tools": true
   }]
   ```

   Match provider and adapter IDs to your configuration. Registry entries do not install adapters or prove executable availability. Lead/review require tier 2; optional frontend tasks need `frontend` capability.
3. Choose **Simple** for the generated explorer → lead → QA → review plan (optionally frontend), or **Expert** for a custom validated dependency graph and configuration. Both share the same runtime, routing, policies, evidence and completion gates. Execution is serial: each advance dispatches at most one ready task; the page loops automatically.
4. Grant `policy.trustedLocalCLI` only for trusted CLIs: they run tasks and acceptance commands with your local user's privileges. Source export needs explicit `policy.sourceExport` consent and an allowed provider. The page derives `allowedProviders` from the registry. Even a provider labeled “local” cannot bypass consent: this executor cannot attest enforced no-network policies and rejects client enforcement claims.
5. Start the mission and inspect its tasks and evidence/configuration viewer.

## Budgets, evidence and recovery

CLI `tokens` and `cost` are `null`: unknown, not zero. Leave `maxTokens` at `0` and `maxCost` omitted or `null`; a positive token limit or any non-null cost limit (even `0`) refuses routing. Execution-count and per-execution timeout limits work; defaults are 12 executions and 600 seconds.

Completion requires clean committed task branches, persisted execution evidence, passing configured QA commands and required structured reviews. Optional `policy.commitTaskChanges` lets the runtime validate permitted paths and commit edits; otherwise agents must commit their allowed changes. Explorer/review remain read-only. Execution records expose session, provider/model, retry, escalation tier, duration, evidence IDs and test/review outcomes. Passing establishes the configured check/review scope, not untested behavior.

Failures raise the routing minimum tier within task-attempt and execution bounds. Review findings can return work to lead within `maxFixes`, followed by fresh QA/review worktrees. Recovery never assumes interrupted execution succeeded. Inspect the failure before using `POST /api/team/missions/{id}/retry`: it accepts only escalated missions below R3 and does not reset attempts or enlarge budgets.

## Risk and isolation limits

R3 requires integration/security checks and two passing reviews. R4 escalates before dispatch and cannot receive automatic approval; high-risk retry requires an external human policy gate. Worktrees and role path checks constrain integrated changes; they are not an OS sandbox. Trusted CLIs retain local-user privileges. Completion yields verified branches for integration, without automatic main merge or production release.

See [runtime details](AI_TEAM_RUNTIME.md) and [model routing](MODEL_ROUTING.md). Current behavior is defined by [model policies](../internal/team/model.go), [application runtime](../internal/app/team.go) and [web API](../internal/web/team.go).
