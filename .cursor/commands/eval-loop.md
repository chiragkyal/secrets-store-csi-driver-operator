---
name: /eval-loop
id: eval-loop
category: Eval Pipeline
description: Run full retrospective eval loop for one feature bundle (Epic Bug Analysis → Eval Generation → baseline)
---

Run the **complete eval improvement loop** for whatever is currently in `evals/inputs/`.

One command. One feature bundle. When done, paste the next bundle into `evals/inputs/` and run again.

## What this command does

```
1. Validate evals/inputs/              (stop if PASTE_ placeholders remain)
2. Load evals/baseline/ + refined-templates/  (round 2+)
3. Epic Bug Analysis                   → evals/outputs/epic-bug-analysis/*
4. Eval Generation
   a. Identify template gaps           → evals/outputs/eval-generation/template-gaps.md
   b. Apply patchable gaps              → refine templates in evals/refined-templates/
   c. Merge eval cases per stage        → evals/baseline/evals/<stage>/<stage>_eval.yaml
   d. Create code-generation evals       → evals/baseline/evals/code-generation/code-generation_eval.yaml
   e. Sync flat stage evals              → schemas/openspec-agile-workflow/evals/<stage>_eval.yaml
   f. Update registry + round snapshot
5. Update round-state                  → increment round, snapshot under baseline/rounds/
```

## Before running

Fill **all** files under `evals/inputs/`:

| File | Paste |
|------|-------|
| `feature-meta.yaml` | Feature name, epic key (optional) |
| `01-ep-ard.md` | EP link + content |
| `02-jira-epic.md` | Epic export |
| `03-original-repo.md` | Pre-feature repo pin |
| `04-user-stories.md` | Stories |
| `05-repo-prs.md` | PR links |
| `bugs/index.yaml` | Bug keys |
| `bugs/<KEY>.md` | One file per bug |

Remove or rename `bugs/PASTE_BUG_KEY_1.md` when adding real bug files.

## Agent instructions

1. Read `evals/pipeline.yaml` for phase order and paths.
2. Read **`evals/epic-bug-analysis/SYSTEM_PROMPT.md`** — execute Epic Bug Analysis fully.
3. Read **`evals/eval-generation/SYSTEM_PROMPT.md`** — execute Eval Generation fully.
4. Do **not** stop between Epic Bug Analysis and Eval Generation unless the user explicitly asks.

### Template path (eval workflow)

| Read / write | Path |
|--------------|------|
| **Eval pipeline templates** | `evals/refined-templates/` only |
| **Do NOT use during eval** | `schemas/openspec-agile-workflow/templates/` |

Seed `evals/refined-templates/` from `schemas/` once on round 1 if empty. All refinements go to `evals/refined-templates/`.

### Consolidated eval files

One YAML per stage — all cases in `evals:` list:

- `evals/baseline/evals/repo-assessment/repo-assessment_eval.yaml`
- `evals/baseline/evals/constitution/constitution_eval.yaml`
- `evals/baseline/evals/plan/plan_eval.yaml`
- `evals/baseline/evals/tasks/tasks_eval.yaml`
- `evals/baseline/evals/implementation/implementation_eval.yaml`
- `evals/baseline/evals/code-generation/code-generation_eval.yaml`

Also sync each merged file to **`schemas/openspec-agile-workflow/evals/<stage>_eval.yaml`** with `template: templates/<name>.md` (forward `/opsx-continue` reads from installed `openspec/schemas/.../evals/`). **code-generation** has no template — sync `code-generation_eval.yaml` for `/opsx-apply` per-task gate.

Do **not** write scattered `eval-r001-*.yaml` per-case files.

### Feedback loop (critical)

| Asset | Round 1 | Round 2+ |
|-------|---------|----------|
| `evals/baseline/evals/<stage>/<stage>_eval.yaml` | Empty → populated | **Read + merge** |
| `evals/baseline/evals-registry.yaml` | Initialized | **Read + append** |
| `evals/refined-templates/` | Seed from schemas → refine | Read **refined** copies → refine again |
| `evals/baseline/routing-learnings.md` | Placeholder → updated | **Read + update** |

Epic Bug Analysis on round 2+ must cross-check bugs against prior evals in `*_eval.yaml` files.

## Outputs

| Location | Content |
|----------|---------|
| `evals/outputs/epic-bug-analysis/` | pattern-analysis, rca-summary, issue-taxonomy |
| `evals/outputs/eval-generation/` | template-gaps, validation-refinements, patches |
| `evals/baseline/evals/<stage>/<stage>_eval.yaml` | Consolidated eval cases per stage |
| `schemas/openspec-agile-workflow/evals/<stage>_eval.yaml` | Forward workflow stage evals (synced from baseline) |
| `evals/baseline/rounds/round-N/` | Round snapshot |
| `evals/refined-templates/*.md` | Refined templates (eval workflow source of truth) |
| `evals/outputs/eval-generation/refinement-patches/` | Diff summary per patched template |
| `evals/baseline/refinement-changelog.md` | Append-only template change log |
| `evals/round-state.yaml` | Incremented round |

## After completion

Tell the user:

> Loop complete (round N). Review `evals/baseline/` and `evals/refined-templates/`. Replace `evals/inputs/` with the next feature bundle and run `/eval-loop` again.

## Guardrails

- Do not use `/opsx-*` commands in this pipeline
- Do not create feature-specific case folders — only generic `evals/inputs/`
- Do not patch `schemas/openspec-agile-workflow/templates/` during eval — use `evals/refined-templates/`
- Do not mark template-gaps Fixed unless `evals/refined-templates/` was actually patched
- Write all eval cases into `<stage>_eval.yaml` — not per-case files
- Do not delete prior eval cases without explicit user approval
- Process bugs one at a time during Epic Bug Analysis
