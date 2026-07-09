# Agent Guidance — Use Root `AGENTS.md`

This project's agent guidance lives in the target repository's root
[`AGENTS.md`](../../AGENTS.md) (symlinked from `CLAUDE.md`), not in this file.

Per the workflow's agents.md resolution order:

1. `{target_repo}/AGENTS.md` ← **use this** (comprehensive, kept up to date with the repo)
2. `{target_repo}/agents.md`
3. `openspec/inputs/agents.md` (this file)
4. `{schema_root}/agents.md` (bundled fallback)

Since `secrets-store-csi-driver-operator/AGENTS.md` already exists at the repo root,
it is resolved automatically ahead of this file for every pipeline stage
(repo-assessment, planning, task creation, implementation). All agents MUST read
and follow that file instead of any content here.

This file is intentionally left as a pointer only. Do not add operator-specific
content here — update the root `AGENTS.md` instead so it stays the single source
of truth for both humans and agents.
