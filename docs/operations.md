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

- `devcycle.db` contains requirements, criteria, tasks, evidence, runs, and Agent session records.
- `devcycle-sessions/*.log` contains local Agent output and is created next to the database.
- Back up the database only after stopping DevCycle, or use SQLite's online backup tooling.
- Sessions still marked `running` after an unclean exit are changed to `interrupted` on the next start.
- Removing a task record never removes its Git worktree from disk. Use `devcycle worktree remove` explicitly.

## Security

- The API can execute reviewed verification commands and start local `codex`, `kimi`, or `claude` processes. Do not expose it to an untrusted network.
- The service hard-rejects non-loopback listeners. Use an SSH tunnel for remote workstations; command execution and Agent APIs have no unauthenticated remote mode.
- Agent prompts are passed to local command-line tools and may be visible to the operating-system process owner.
- Evidence and logs may include source paths or command output; review them before sharing.
