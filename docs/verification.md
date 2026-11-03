# Verification against real Claude Code sessions

Recorded on 2026-11-03 with Claude Code 2.1.317 (`claude -p`, model `sonnet`, `--setting-sources project --strict-mcp-config`, so only the test project's settings and hook were active). The project had one check:

```yaml
checks:
  answer:
    run: "grep -qx 42 answer.txt"
    inputs: ["**"]
```

The prompt never mentioned naru: *"Write the product of 6 and 7 as a bare number into answer.txt. That's the whole task."*

| Run | Setup | What happened | Turns | Cost |
| --- | --- | --- | --- | --- |
| 1 | Hook with an absolute path; `Bash(naru:*)` allowed | Stop blocked ("never run"). The agent called naru by the absolute path shown in the hook feedback, which missed the allow rule, so every attempt was denied. With the pre-breaker build the gate blocked **68 times**; Claude Code's own cap kept resetting because the agent called tools between stops. | 94 | $0.58 |
| 2 | `naru gate stop` on `PATH`; `Bash(naru check:*)` allowed | Stop 1 blocked ("never run") → agent ran `naru check answer` → PASS → Stop 2 allowed. | 4 | $0.01 |
| 3 | `naru` denied; breaker build (`max_repeats` 3) | Stops 1–3 blocked with identical reasons; the agent said it could not proceed. Stop 4 allowed with "NOT VERIFIED — the same block repeated 3 times with no progress". | 6 | $0.02 |

Changes made because of these runs:

- The no-progress breaker (`gate.max_repeats`), from run 1.
- The README now uses the bare `naru gate stop` command and shows the permission allow rules, from runs 1 and 2.

Not yet verified with a live agent: Codex integration, stale receipts after edits made later in a session (covered by unit and integration tests), and long-running checks.
