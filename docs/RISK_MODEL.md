# Risk policy

| Level | Trigger examples | Required workflow |
|---|---|---|
| R0 | declared documentation-only file scope | build + machine test |
| R1 | ordinary feature | build + test + independent review |
| R2 | cross-module/large change | plan + build + test + review + bounded fix/regression |
| R3 | auth, DB, payment, security, migration, critical API | plan + build + two reviews + configured integration/security checks + candidate |
| R4 | production, destructive/irreversible action, credentials | plan/verify/candidate externally; human-controlled approval, no automatic dispatch |

Assessment is conservative keyword/path rules with recorded reasons, not an AI safety
classifier. A user cannot lower the automatically assessed floor. The runtime reassesses
actual changed paths and pauses when risk increases into a policy needing review.
Simple currently defaults to R1 when it cannot prove a documentation-only scope; it may
over-classify documentation discussing auth or production. This is visible in Expert.

R3 without explicit integration/security commands escalates before dispatch; Simple
adds a second independent review task. Expert supplies actual relevant checks. Review
sessions have distinct identity from authors; a separate model/provider is recommended
when available. A structured verdict with findings fails review; process exit or an
agent's assertion of completion cannot substitute for required evidence.

R4 is not approved by a boolean API field or an AI-generated human-override record.
Production deployment requires a separately authenticated human workflow. The current
deployment extension describes plans and does not execute infrastructure changes.
