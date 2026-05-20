# Agents

The `sprawl-bookkeeper` sub-agent comes in three host flavours because each AI
tool uses a different schema. There is no installer — drop the file in the
right place by hand. Each host loads sub-agents the same way it loads them
when you author one yourself.

| Host | File | Global path | Project-local path |
|------|------|-------------|--------------------|
| Claude Code | [`claude/sprawl-bookkeeper.md`](claude/sprawl-bookkeeper.md) | `~/.claude/agents/sprawl-bookkeeper.md` | `<repo>/.claude/agents/sprawl-bookkeeper.md` |
| OpenCode | [`opencode/sprawl-bookkeeper.md`](opencode/sprawl-bookkeeper.md) | `~/.config/opencode/agents/sprawl-bookkeeper.md` | `<repo>/.opencode/agents/sprawl-bookkeeper.md` |
| Codex | [`codex/sprawl-bookkeeper.toml`](codex/sprawl-bookkeeper.toml) | `~/.codex/agents/sprawl-bookkeeper.toml` | `<repo>/.codex/agents/sprawl-bookkeeper.toml` |

For example, to install for Claude Code globally:

```sh
mkdir -p ~/.claude/agents
curl -fsSL https://raw.githubusercontent.com/ultrakorne/sprawl_cli/master/agents/claude/sprawl-bookkeeper.md \
  -o ~/.claude/agents/sprawl-bookkeeper.md
```

The agent calls the `sprawl` CLI; install the [`sprawl` skill](../skills/sprawl)
alongside it via `gh skill install ultrakorne/sprawl_cli sprawl`.
