# Capability registry and model routing

Registry entries identify model ID, provider, configured CLI adapter, capabilities,
context limit, tier (0 cheap, 1 normal, 2 strong), tool support, availability, optional
cost-per-million metadata and historical evaluation score. Capability values are
coding, reasoning, planning, frontend, vision, long_context, review, testing,
exploration and low_cost. Unknown/duplicate entries and malformed DAG/config fail
validation. Business workflows contain no vendor-name branches.

An AI Team adapter must contain exactly one `{{prompt}}` or `{{prompt_stdin}}` and an explicit `{{model}}`
in argv; model selection is substituted into argv, never a shell string. Existing
standalone adapters retain their behavior. Configure a model-aware adapter in the
existing DevCycle settings, for example command `codex`, args
`["exec", "--model", "{{model}}", "--sandbox", "workspace-write", "{{prompt_stdin}}"]`.
Equivalent Kimi/DeepSeek/Claude/local adapters use their actual CLI syntax or an
explicitly configured agent; no provider credential is stored in a registry entry.
This is configuration support, not a claim each vendor has been live-tested here.

Routing filters provider allowlist, explicit source-export consent, live executable
availability, enabled state, capabilities, context bound, tool and enforcement policy.
Lead/review and R3 require strong tier. Exploration/QA prefer cheaper eligible tiers.
Failures raise the minimum tier; exhaustion escalates to the policy owner. Historical
same-role/model outcomes rank otherwise eligible models; no automatic learning system
or policy override is implemented. The prompt byte bound conservatively reserves
context for memory and prior handoffs, rather than pretending to know tokenization.

CLI token/cost usage is `null` when unreported. Any strict cost budget or positive token
budget refuses unmetered CLI execution; execution-count and timeout bounds still work.
No zero-dollar usage or exact token accounting is invented. A future metered executor
must reserve worst-case usage before requests and settle measured usage afterward.

Explicit provider configuration and OS-keyring/environment secret storage, masking,
replacement, revocation, connection tests and redacted preview reuse v1 services.
Source export is disabled unless the mission explicitly allows its registered provider. A CLI labeled local still needs explicit source/network consent unless an enforcing no-network executor exists; client labels cannot bypass consent.
Tool/network sandbox capability cannot be self-attested by client registry metadata.

Use `{{prompt_stdin}}` for large contexts: it expands to `-` and streams the prompt to stdin without shell interpolation or Windows argv limits. The configured CLI must support this stdin convention. Existing `{{prompt}}` argv adapters remain supported.
