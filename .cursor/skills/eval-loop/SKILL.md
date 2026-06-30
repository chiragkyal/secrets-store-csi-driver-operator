---
name: eval-loop
description: Run full retrospective eval loop for one feature bundle. Use for /eval-loop.
license: MIT
metadata:
  author: openspec
  version: "1.1"
---

Single command for the eval improvement pipeline. One feature bundle per invocation.

## When to use

User runs `/eval-loop` after pasting a feature bundle into `evals/inputs/`.

## Execution

1. Read `evals/pipeline.yaml`
2. Validate `evals/inputs/` — halt on `PASTE_` placeholders
3. Load `evals/baseline/`, `evals/refined-templates/`, and `evals/round-state.yaml`
4. Follow `evals/epic-bug-analysis/SYSTEM_PROMPT.md` → write `evals/outputs/epic-bug-analysis/*`
5. Follow `evals/eval-generation/SYSTEM_PROMPT.md`:
   - Templates: read/write **`evals/refined-templates/` only** (not `schemas/`)
   - Identify gaps → patch refined-templates
   - Merge evals into **`evals/baseline/evals/<stage>/<stage>_eval.yaml`** (one file per stage)
   - Author **code-generation** evals → `evals/baseline/evals/code-generation/code-generation_eval.yaml`
   - Sync flat copies to **`schemas/openspec-agile-workflow/evals/<stage>_eval.yaml`** (`template: templates/...`; code-generation has no template)
   - Update baseline registry
6. Increment round in `evals/round-state.yaml`

## Template path

**Eval workflow:** `evals/refined-templates/` — read and write.

**Do not** patch `schemas/openspec-agile-workflow/templates/` during eval. Seed refined-templates from schemas on round 1 if empty.

## Consolidated eval files

| Stage | File |
|-------|------|
| repo-assessment | `evals/baseline/evals/repo-assessment/repo-assessment_eval.yaml` |
| constitution | `evals/baseline/evals/constitution/constitution_eval.yaml` |
| plan | `evals/baseline/evals/plan/plan_eval.yaml` |
| tasks | `evals/baseline/evals/tasks/tasks_eval.yaml` |
| implementation | `evals/baseline/evals/implementation/implementation_eval.yaml` |
| code-generation | `evals/baseline/evals/code-generation/code-generation_eval.yaml` |

## Feedback loop

- Round 2+ reads `*_eval.yaml` and `evals/refined-templates/` in both phases
- Templates accumulate refinements under `evals/refined-templates/`

## Do not

- Split into multiple commands
- Patch schemas/ during eval workflow
- Write per-case `eval-r*.yaml` files — use consolidated `*_eval.yaml`
- Skip Eval Generation after Epic Bug Analysis
