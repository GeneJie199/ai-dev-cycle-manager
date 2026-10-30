# Project memory

Memory is structured project knowledge, separate from chat history and execution
evidence. Kinds: architecture, product, rejected_option, constraint, convention,
api_contract, environment, release, risk, failure and user_decision.

Each record has project ID, stable record ID, kind, content, source/provenance, author,
revision and update timestamp. A PUT must include the last revision; a stale edit is
rejected rather than losing someone else's decision. New revisions append to the
auditable history. GET returns current records; `?history=true` returns all revisions.
Project filtering prevents accidental cross-project retrieval.

The mission snapshots current project memory at creation and passes it, marked as
data rather than instructions, to task contexts. Later memory edits remain reviewable
and affect future missions; they do not silently change an executing plan. Inputs are
bounded and use the existing secret redactor. Store explicit decisions with a source
such as an ADR or reviewed requirement. No vector store, opaque long-term AI memory,
automatic chat ingestion or uncontrolled model-generated memory write is introduced.
