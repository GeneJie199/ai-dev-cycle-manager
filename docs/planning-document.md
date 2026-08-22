# Planning Document v1

`devcycle.planning-document/v1` is the provider-neutral, editable plan owned by one requirement. Manual and AI-assisted planning use the same document, validation, revision, persistence, and application path.

## Fields

| Field | Meaning |
|---|---|
| `understanding` | concise statement of the requested outcome |
| `scope.included` / `scope.excluded` | explicit delivery boundary |
| `assumptions` | assumptions used to make the plan actionable |
| `openQuestions` | question, blocking flag, and optional safe default |
| `criteria` | testable acceptance condition and rationale |
| `testCases` | title, linked criterion, kind, setup, steps, and expected results |
| `testStrategy` | summary, environments, and deterministic commands |
| `tasks` | ordered tasks, title-based dependencies, rationale, suggested adapter, and deliverables |
| `risks` | risk, severity, and mitigation |
| `rollbackConcerns` | data or operational concerns that affect rollback |
| `candidateNotes` | information carried into release-candidate review |
| `source` / `provider` | `manual` or `ai` provenance and optional provider ID |
| `status` | `draft` or `applied` |
| `revision` | optimistic concurrency revision |

## Lifecycle

1. `GET /api/requirements/{id}/plan` returns the current document or 404 when none exists.
2. `PUT /api/requirements/{id}/plan` validates and saves a draft. The submitted `revision` must match the stored revision; stale edits receive HTTP 409.
3. `POST /api/requirements/{id}/ai-plan` returns an editable AI draft and does not create criteria or tasks.
4. `POST /api/requirements/{id}/ai-plan/apply` with `{ "document": ... }` atomically saves the applied document and creates its acceptance criteria, tasks, and dependencies.

Application is all-or-nothing. Missing dependencies, invalid fields, stale revisions, or database failures leave both the document and delivery graph unchanged. An applied document remains available as an audit snapshot and cannot be silently overwritten as a new draft.

The older selective `{ "criteria": [...], "tasks": [...] }` apply body remains accepted for compatibility, but new clients should send the complete document.
