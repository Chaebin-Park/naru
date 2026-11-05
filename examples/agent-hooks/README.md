# Agent hook templates

These files are examples, not active configuration. Reading this directory does
not install hooks or change trust, permissions, or user settings.

## Compatibility and evidence

The templates were compared with official documentation on 2026-11-05, with
Claude Code 2.1.318 and Codex CLI 0.166.0 installed locally. Local tests parse the
templates and replay their commands against NARU. This does not prove that a
real agent session loads or enforces the hooks. Current live evidence and gaps
are recorded in [verification](../../docs/verification.md).

Claude uses `PreToolUse` tool-name matchers and a `Stop` event. The template
covers `Write`, `Edit`, `NotebookEdit`, and `Bash`. Both handlers are synchronous;
background hooks cannot block a tool or completion. See the
[Claude hook reference](https://code.claude.com/docs/en/hooks).

Codex uses `Bash` for shell calls and `apply_patch` for patches. Both payloads
carry `tool_input.command`. A `Write|Edit` matcher does not convert a patch into
Claude file-path fields. `write_stdin` does not trigger another `PreToolUse`
check. Codex requires workspace and hook trust before running workspace hooks.
See the [Codex hook reference](https://learn.chatgpt.com/docs/hooks).

## Prepare the command

Use a reviewed NARU binary available as `naru` on the agent process's `PATH`.
Check `command -v naru` in that environment, then run `naru status` from the
intended project. A missing receipt is expected before the first `naru check`.
An interactive terminal and a desktop app may inherit different paths.

Keep the bare `naru` commands in both templates. Claude's optional allow rules
cover `naru check` and `naru status`; an absolute binary path does not match
those rules. These permissions do not approve arbitrary shell commands, mode
changes, hook installation, or trust changes. Review the project's check
commands before permitting `naru check`: it runs those commands.

## Add hooks without replacing settings

1. Inspect and back up the destination before editing it. Review the template
   and obtain any required approval for the target project.
2. For Claude, use [claude.settings.json](claude.settings.json) as a fragment for
   the project's `.claude/settings.json`. Keep all existing top-level keys.
   Append the NARU entries to the existing `hooks.PreToolUse` and `hooks.Stop`
   arrays only if equivalent entries are absent. Preserve other handlers and
   matchers, including those inside a shared entry. If wanted, add only the two
   missing allow rules to `permissions.allow`; preserve deny and ask rules.
   See [Claude setting scopes](https://code.claude.com/docs/en/settings).
3. For Codex, merge [codex.hooks.json](codex.hooks.json) into the project's
   `.codex/hooks.json` in the same way. Do not add Claude permission syntax to
   Codex configuration. Inspect other configuration layers for duplicate NARU
   hooks. Preserve existing Codex approval and sandbox settings.
4. Validate the resulting JSON. Review the diff before saving it. Follow the
   agent's normal hook reload and trust review flow; do not bypass trust or
   assume an already running session has loaded the changes.
5. In a disposable project, explicitly verify missing, pass, stale, and
   `NOT VERIFIED` outcomes in each real agent before claiming live support.
   Record agent version, NARU commit, isolated configuration, and observed
   decisions in the verification record. Command replay alone is insufficient.

Do not overwrite a whole settings file with an example. Do not add `async` to
these handlers or pipe their output through a formatter: the exit code and
stdout/stderr form part of the gate contract.

## Remove only the integration you added

Compare the current settings with your backup. Remove only the command handlers
`naru gate pre-tool` and `naru gate stop` that you added. Keep other handlers in
the same entry; remove an entry only when its handler array becomes empty.
Remove the two Claude allow rules only if this installation added them and
they are no longer needed. Preserve unrelated keys, permissions, and hooks.
Validate the JSON and review the diff, then reload through the agent's normal
flow. Do not restore an old backup over newer unrelated edits.

## Manual fallback

If native hooks are unavailable or not approved, merge this instruction into
the project's existing `AGENTS.md` (or the agent's instruction file):

```text
Before reporting completion, run `naru status` from the project root.
If it exits non-zero, run `naru check`, fix failures, and repeat.
If verification cannot finish, report NOT VERIFIED and the reason.
Do not edit .naru records or change verification mode to manufacture a pass.
```

This is an agent instruction, not automatic enforcement. The pre-tool guard is
also a heuristic guardrail, not a filesystem security boundary. Keep CI as the
authoritative shared check. NARU's no-progress escape reports `NOT VERIFIED`;
it does not create passing evidence.
