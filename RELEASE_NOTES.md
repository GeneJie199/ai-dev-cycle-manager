# AI Engineering Team v2.0.0-rc.2 Community

This candidate adds a guided Web-first Provider/project/Mission setup, explicit
Ready / Configuration Required / Unsupported / Unavailable states, configured
role routing, and a Delivery view backed by the existing real Mission evidence.
API structured-edit execution reuses Agent sessions, isolated Git worktrees,
controller-owned checks and strict independent review. Public v1 API and schema 3
remain available; new registry and budget data use the existing Team sidecar.

API generation, connection tests and model discovery require explicit bounded
request authorization. Requests reserve worst-case configured CNY cost and token
limits before sending; missing or invalid usage retains the entire reservation
and blocks further requests. Costs use user-configured conservative rates and
are estimates, not provider invoices. Metadata requests count toward the call
limit and have no generation token charge. No search/fetch tools are exposed.
Credentials use the existing OS keyring/environment service, or an explicitly
selected Linux owner-only 0700/0600 private-file backend. Private files are not
OS encryption. Windows uses Credential Manager. API permissions are distinct
from chat subscriptions. No model name, real credential, three-provider success,
24-hour stability result, or GA approval is bundled or fabricated.

This is a release candidate for trusted single-user local environments. Generic
CLI adapters execute with the host user's permissions; worktrees and role path
checks are not an OS sandbox. Enforcing isolation policies fail closed. R4 is
blocked. ReleaseGuard GO grants no deployment permission. Production deployment,
untrusted multi-tenancy, invoicing, HA and marketplace integrations are not included.
Unmetered CLI usage is unknown, not an actual invoice; strict monetary/token
budgets reject unmetered adapters. Model access requires the user's own local CLI
configuration and source-export consent; no credentials are bundled.

Install using the Suite offline archive for the verified platform, verify its
SHA-256 against checksums.txt, extract it and follow docs/offline-install.md.
Use an isolated installation prefix for evaluation. Existing v1 installations
must back up configuration and data before upgrade; use the Suite installer with
the new manifest. Run doctor and restart after upgrade. Suite rollback restores
the saved binaries/configuration; DevCycle retains original database schema 3
and keeps new Team state in a separate sidecar. Preserve the sidecar across a
rollback and do not let old binaries manage Team state.

New six-repository source is AGPL-3.0-only. Historical v1.x remains under its
original Apache-2.0 terms. Archives include license texts, third-party notices,
SBOM and a separately checksummed Corresponding Source archive with vendored Go
dependencies. Commercial use under AGPL is allowed when its terms are met.
Commercial alternative terms remain subject to verified rights and a separate
agreement; no signed CLA, copyright transfer or third-party relicensing right
is asserted. The contributor policy is not evidence of a signed CLA.

Platform support and precise source revisions are recorded in the Suite release
manifest and final acceptance report. Cross-compilation alone is not native
installation verification. Security reports: see SECURITY.md.

Release security: affected products select edwards25519 v1.1.1 (GO-2026-4503 fix). Every published target binary must pass govulncheck binary scanning before upload.
