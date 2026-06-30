# Stage evals — openspec-agile-workflow forward workflow

Merged eval rubrics for `/opsx-continue` artifact gates. Installed to:

`openspec/schemas/openspec-agile-workflow/evals/`

| File | Stage |
|------|-------|
| `repo-assessment_eval.yaml` | repo-assessment |
| `constitution_eval.yaml` | constitution |
| `plan_eval.yaml` | plan |
| `tasks_eval.yaml` | tasks |
| `implementation_eval.yaml` | implementation |
| `code-generation_eval.yaml` | code-generation (per-task during /opsx-apply) |

Assertion schemas: `evals/stages/<stage>/eval-spec.yaml`  
Gate instructions: `../stage-gate/SYSTEM_PROMPT.md`

**Not** under `evals/baseline/` — that path is for `/eval-loop` retrospective history only.
When `/eval-loop` merges new cases, it syncs updated `*_eval.yaml` files here and to `evals/baseline/evals/`.
