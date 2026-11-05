# AI Engineering Team v2.0.0-rc.3 Community

Delivery now opens for pending and R4-blocked Missions with no execution
records. Users can review the incomplete work and risks and download the
evidence ZIP. The release status still reflects the Mission's actual readiness.

Open `/suite.html` for the work entry, `/team.html` for guided setup and AI Team,
or `/` for the full DevCycle workbench. Suite, Team, Provider, Delivery and the
existing workbench views use shared light/dark/system themes, persistent theme
selection and responsive navigation. Charts use recorded product data and explicit
empty/error states. Scan, monitoring and release-validation links retain the
installation's actual host/ports; each product remains independently usable.
The deployment entry describes installation and project boundaries; business
deployment from the page is unavailable.

Guided Provider/project/Mission forms replace manual registry JSON for normal
first use. Ready / Configuration Required / Unsupported / Unavailable states
identify connection and platform constraints. New Web-created projects preserve
the effective Git identity and fill only missing name/email fields in their own
repository configuration, so later controller commits in worktrees work on a
fresh machine. Existing user/global Git configuration remains intact.

Provider setup offers official Codex CLI adaptation, API credentials, explicit
model discovery and connection tests, role mapping, revocation and usage limits.
Official CLI installation and login remain the CLI's responsibility. Chat
subscriptions and API permissions are separate; model names must be discovered
or verified. Missing Kimi authorization is Configuration Required / BLOCKED.
There is no implicit switch to a paid Provider.

API structured edits reuse the existing AgentSession, isolated Git worktrees,
controller checks, review and source-bound evidence. Guided Missions allow eight
executions and one fix by default. Delivery shows actual completed/incomplete
work, changed files, diffs, tests, review attempts and risks, with an evidence ZIP
and release-candidate data. Failed attempts remain visible. Preview/start
instructions are unavailable unless verified; a ready candidate grants no
deployment permission. Public v1 API and schema 3 remain available; new registry
and budget data use the existing Team sidecar.

API generation, connection tests and model discovery require explicit bounded
request authorization. Requests reserve worst-case configured CNY cost and token
limits before sending; missing or invalid usage retains the entire reservation
and blocks further requests. Costs use user-configured conservative rates and
are estimates, not provider invoices. Metadata requests count toward the call
limit without claiming generation tokens. The API edit adapter has no shell,
search, fetch or deployment tools. Endpoint/model/credential snapshots and pinned
Mission bindings prevent setup changes from authorizing a different endpoint
during dispatch. Truncated, filtered, refused, tool-call and incomplete responses
fail even when their text contains valid-looking JSON.
Credentials use the existing OS keyring/environment service, or an explicitly
selected Linux owner-only 0700/0600 private-file backend. Private files are not
OS encryption. Windows uses Credential Manager. API permissions are distinct
from chat subscriptions. This RC does not assert three-model live collaboration,
a completed 24-hour stability window, or GA approval. Exact tested source,
provider and platform scope belong to the separate acceptance evidence.

This is a release candidate for trusted single-user local environments. Generic
CLI adapters execute with the host user's permissions; worktrees and role path
checks are not an OS sandbox. Enforcing isolation policies fail closed. R4 is
blocked. ReleaseGuard GO grants no deployment permission. Production deployment,
untrusted multi-tenancy, invoicing, HA and marketplace integrations are not included.
Unmetered CLI usage is unknown, not an actual invoice; strict monetary/token
budgets reject unmetered adapters. Model access requires the user's own local CLI
configuration and source-export consent; no credentials are bundled.

Install using the Suite offline archive for the verified platform, verify its
SHA-256 against `checksums.txt`, extract it and follow the
[Suite offline installation guide](https://github.com/GeneJie199/ai-devops-open-suite/blob/main/docs/offline-install.md).
Use an isolated installation prefix for evaluation. Existing v1 installations
must back up configuration and data before upgrade; use the Suite installer with
the new manifest. Run doctor and restart after upgrade. Suite rollback restores
the saved binaries/configuration; DevCycle retains original database schema 3
and keeps new Team state in a separate sidecar. Preserve the sidecar across a
rollback and do not let old binaries manage Team state.

New six-repository source is AGPL-3.0-only. Historical v1.x remains under its
original Apache-2.0 terms. Platform archives preserve license texts, notices,
`third-party-inventory.json` and `third-party.cdx.json`. These two inventories
cover the Suite and may include dependencies not linked into standalone DevCycle;
they are not a per-binary dependency statement. The release workflow separately
generates `devcycle-<tag>.cdx.json` from this repository's Go module graph and
publishes a checksummed Corresponding Source archive with vendored dependencies.
Commercial use under AGPL is allowed when its terms are met.
Commercial alternative terms remain subject to verified rights and a separate
agreement; no signed CLA, copyright transfer or third-party relicensing right
is asserted. The contributor policy is not evidence of a signed CLA.

Platform support and precise source revisions are recorded in the Suite release
manifest, [support matrix](https://github.com/GeneJie199/ai-devops-open-suite/blob/main/SUPPORT_MATRIX.md)
and final acceptance report. Historical stable-release verification does not
establish this candidate's native acceptance. Cross-compilation alone is not
native installation verification. Security reports: see the
[DevCycle security policy](https://github.com/GeneJie199/ai-dev-cycle-manager/blob/main/SECURITY.md).

Each published target binary must pass the release workflow's actual-binary
govulncheck scan before upload. Published checksums and source provenance identify
the precise artifacts; these notes do not substitute for the scan results.
