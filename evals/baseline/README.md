# Baseline — cumulative feedback loop store

Updated after each `/eval-loop`. Fed as input to Epic Bug Analysis and Eval Generation on subsequent rounds.

| Path | Purpose |
|------|---------|
| `evals/<stage>/<stage>_eval.yaml` | **One consolidated file per stage** — all eval cases in `evals:` list |
| `evals-registry.yaml` | Master index (`stage_eval_files` + per-eval metadata) |
| `routing-learnings.md` | Bug-derived guardrails from `/eval-loop` (not workflow `agents.md`) |
| `refinement-changelog.md` | Append-only log of template changes |
| `rounds/round-N/` | Snapshot per completed loop |

**Templates** are stored in `evals/refined-templates/` — not here, not in `schemas/`.
