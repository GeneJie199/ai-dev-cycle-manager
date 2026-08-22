# AI and Agent Configuration

DevCycle works without AI. Configure models only when you want assisted planning or Git explanation, and configure Agent adapters only when you want DevCycle to launch a coding agent in a task Worktree.

## AI providers

The Settings page discovers local Kimi, Codex, and Claude planning CLIs and supports these API protocols:

| Kind | Default endpoint | Credential |
|---|---|---|
| OpenAI Compatible | `https://api.openai.com/v1/chat/completions` | `Authorization: Bearer ...` |
| Anthropic | `https://api.anthropic.com/v1/messages` | `x-api-key` |
| Gemini | `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent` | `x-goog-api-key` |
| Local OpenAI / Ollama | `http://127.0.0.1:11434/v1/chat/completions` | none by default |
| Custom OpenAI | user supplied | optional user-specified credential header |

Set an ID, display name, model, optional base URL/path, timeout from 5 to 600 seconds, and either:

- a key entered in the UI, which is stored in the operating-system credential store; or
- an uppercase environment variable name such as `OPENAI_API_KEY`, whose value is read only when making a request.

The SQLite row stores only a non-secret reference. API responses expose whether a credential is configured, never the reference or value. Replacing or deleting a provider cleans up the old keyring entry.

Before an API request is sent, DevCycle shows the destination, model, protocol, redacted input, header names, and data that is included or excluded. Secret values, private keys, unselected files, the full process environment, Git credentials, and database business rows are excluded.

Credentialed remote endpoints must use HTTPS. HTTP is accepted only for loopback endpoints. URLs cannot contain embedded credentials, query strings, or fragments; redirects are not followed; response size and request duration are bounded.

## Agent adapters

Built-in adapters are:

```text
Codex       codex exec {{prompt}}
Claude Code claude -p {{prompt}}
Gemini CLI  gemini -p {{prompt}}
Kimi Code   kimi -p {{prompt}}
OpenCode    opencode run {{prompt}}
```

The Settings page reports whether each executable is available. A session can start only for a task with an associated Worktree.

A custom adapter consists of an executable and 1 to 30 arguments. The arguments must contain exactly one `{{prompt}}` placeholder. DevCycle expands only that placeholder and starts the executable directly with an argv array; it does not invoke a Shell and does not accept adapter-specific environment variables or embedded secrets.

## Task handoffs

A handoff records the source session, destination adapter, summary, completed work, remaining work, risks, validation steps, and changed files. It is stored independently from the source process and can be accepted later, so changing Agent tools does not lose delivery context.

Prompts, logs, handoffs, and provider error bodies pass through the same secret redaction rules before durable storage or display.
