# DevCycle Operations

DevCycle is a per-developer tool. Run it as the same user who owns the Git repositories and keep the workbench on loopback.

## Install

```bash
go build -trimpath -o devcycle ./cmd/devcycle
sh ./scripts/install.sh ./devcycle
```

The default user installation is `~/.local/bin/devcycle`; persistent data belongs in `~/.local/share/devcycle`.

## Run interactively

```bash
devcycle serve \
  --db ~/.local/share/devcycle/devcycle.db \
  --addr 127.0.0.1:8766
```

Open `http://127.0.0.1:8766/`. To reach a remote workstation, keep the service on loopback and use an SSH tunnel:

```bash
ssh -L 8766:127.0.0.1:8766 user@workstation
```

## Optional systemd user service

```bash
mkdir -p ~/.config/systemd/user
install -m 0644 deploy/devcycle.service ~/.config/systemd/user/devcycle.service
systemctl --user daemon-reload
systemctl --user enable --now devcycle
```

## Data and recovery

- `devcycle.db` contains requirements, planning documents, criteria, tasks, dependencies, handoffs, provider metadata, adapter metadata, evidence, runs, and Agent session records.
- `devcycle-sessions/*.log` contains local Agent output and is created next to the database.
- API credential values are not in the database. Keyring credentials live in the operating-system credential store under service `ai-devops-devcycle`; environment-backed credentials remain in the configured process environment.
- Back up the database only after stopping DevCycle, or use SQLite's online backup tooling.
- Restore the database with DevCycle stopped. On first open, older supported schemas migrate transactionally to the current schema; a database from a newer unsupported version is rejected instead of being modified.
- Sessions still marked `running` after an unclean exit are changed to `interrupted` on the next start.
- Removing a task record never removes its Git worktree from disk. Use `devcycle worktree remove` explicitly.
- Removing an API provider also removes its keyring credential. Environment variables are never changed by DevCycle.

## Security

- The API can execute reviewed verification commands and start configured local Agent processes. Do not expose it to an untrusted network.
- The service hard-rejects non-loopback listeners. Use an SSH tunnel for remote workstations; command execution and Agent APIs have no unauthenticated remote mode.
- Agent prompts are passed as one direct process argument and may be visible to the operating-system process owner. Custom adapters are executed without a Shell.
- Credentialed remote AI providers require HTTPS. Redirects are rejected, output is bounded, and secret-looking content in prompts, logs, handoffs, and provider errors is redacted.
- Evidence and logs may include source paths or command output; review them before sharing.
