# Eval pipeline — retrospective workflow improvement

Continuous improvement loop for **openspec-agile-workflow**: derive evals from completed feature bundles (EP + epic + stories + PRs + bugs), refine templates, and accumulate learnings for the next bundle.

**Stage evals for `/opsx-continue`** ship with the schema package — not under `evals/`:

`schemas/openspec-agile-workflow/evals/*_eval.yaml` → installed as `openspec/schemas/openspec-agile-workflow/evals/`

`/eval-loop` merges cases into `evals/baseline/evals/` **and** syncs flat copies to the schema `evals/` directory.

---

## Getting Started for New Operators

This workflow is designed to be used across multiple operators. Each operator team clones this repo and customizes it for their specific operator.

### Setup steps

1. **Clone this repo**
   ```bash
   git clone <this-repo-url>
   cd openspec-repo
   ```

2. **Edit `schemas/openspec-agile-workflow/agents.md` for your operator**
   This is the **only operator-specific file** in the schema. It defines:
   - Your operator's architecture, components, and knowledge graph
   - Execution agent IDs and routing rules for task assignment
   - Testing instructions specific to your operator
   - PR hygiene and dev environment details

   All other templates in `schemas/openspec-agile-workflow/templates/` are generic and work for any operator.

3. **Fill `evals/inputs/` with your feature bundle data**
   Replace the placeholder content in each file:
   | File | What to paste |
   |------|---------------|
   | `feature-meta.yaml` | Feature name, epic key, target repo URL |
   | `01-ep-ard.md` | Enhancement Proposal / ARD content |
   | `02-jira-epic.md` | Jira epic export |
   | `03-original-repo.md` | Pre-feature repo state (commit, branch, key files) |
   | `04-user-stories.md` | User stories linked to the epic |
   | `05-repo-prs.md` | PR links and diffs for the completed feature |
   | `bugs/index.yaml` | Bug keys list |
   | `bugs/<KEY>.md` | One file per bug |

4. **Run `/eval-loop` to generate evals tailored to your operator**
   ```
   /eval-loop
   ```
   This populates `evals/baseline/`, `evals/refined-templates/`, and syncs stage evals to `schemas/.../evals/`.

5. **Repeat** — replace `evals/inputs/` with the next feature bundle and run `/eval-loop` again.

---

## One command, one feature bundle

```
Paste inputs → /eval-loop → baseline updated → paste next bundle → /eval-loop again
```

| Step | Action |
|------|--------|
| 1 | Fill `evals/inputs/` with one feature bundle (links + exports) |
| 2 | Run **`/eval-loop`** in Cursor |
| 3 | Review `evals/baseline/` and `evals/refined-templates/` |
| 4 | Replace `evals/inputs/` with the next feature bundle |
| 5 | Run **`/eval-loop`** again — prior evals and refined templates are auto-loaded |

## Data flow

```
evals/inputs/  ──────────────────►  Epic Bug Analysis  ──►  evals/outputs/epic-bug-analysis/*
                                              │
evals/refined-templates/  ───────┐          │
baseline/evals/*_eval.yaml  ───┤          ▼
baseline/routing-learnings.md  ───────────┴────►  Eval Generation
                                              │
                                              ├──► evals/baseline/evals/<stage>/<stage>_eval.yaml
                                              ├──► PATCH evals/refined-templates/
                                              └──► evals/baseline/routing-learnings.md
```

**Round 2+:** Eval Generation reads **refined templates** and **consolidated stage eval files** from prior rounds.

## Template paths

| Path | Role |
|------|------|
| `schemas/openspec-agile-workflow/templates/` | Upstream defaults for forward `/opsx-*` workflow — **not** eval pipeline input |
| `evals/refined-templates/` | **Eval workflow read/write** — cumulative template refinements |

On round 1 (empty `refined-templates/`), seed once from `schemas/`, then refine only under `evals/refined-templates/`.

### Template refinement (mandatory for patchable gaps)

| Gap type | Action |
|----------|--------|
| `patchable` | Update file in `evals/refined-templates/` |
| `eval-only` | Eval YAML only — document why in `template-gaps.md` |
| `deferred` | Open question — do not mark Fixed |

Audit trail: `evals/outputs/eval-generation/refinement-patches/` + `evals/baseline/refinement-changelog.md`

## Consolidated eval files (one per stage)

All eval cases for a stage live in a **single YAML file**:

| Stage | File |
|-------|------|
| repo-assessment | `evals/baseline/evals/repo-assessment/repo-assessment_eval.yaml` |
| constitution | `evals/baseline/evals/constitution/constitution_eval.yaml` |
| plan | `evals/baseline/evals/plan/plan_eval.yaml` |
| tasks | `evals/baseline/evals/tasks/tasks_eval.yaml` |
| implementation | `evals/baseline/evals/implementation/implementation_eval.yaml` |
| code-generation | `evals/baseline/evals/code-generation/code-generation_eval.yaml` |

Each file contains an `evals:` list with all cases (round 1, round 2, ...). Do **not** scatter per-case `eval-r*.yaml` files.

**code-generation** cases are tagged with `oape_command` and run during **`/opsx-apply`** per task (not during `/opsx-continue`).

## Forward workflow (`/opsx-continue`) — artifact eval gate

After each artifact is generated (templates from `{schema_root}/templates/`):

```
generate v1 → run stage evals → refine artifact → user approval → next /opsx-continue
```

| Score with | Refine |
|------------|--------|
| `openspec/schemas/openspec-agile-workflow/evals/<stage>_eval.yaml` | Change artifact under `openspec/changes/<name>/` only |

**Do not** edit `{schema_root}/templates/` or `evals/refined-templates/` during `/opsx-continue`.

Instructions: `{schema_root}/stage-gate/SYSTEM_PROMPT.md`, `{schema_root}/stage-gate/artifact-eval-map.yaml`  
Results: `openspec/changes/<change>/eval-results/<artifact-id>.yaml`

## Forward workflow (`/opsx-apply`) — code generation eval gate

After each task's OAPE command (or manual work) and verification:

```
execute → verify → run code-generation evals → refine code until pass → user approves code → task report → next task
```

| Score with | Refine | Record |
|------------|--------|--------|
| `openspec/schemas/.../evals/code-generation_eval.yaml` (by `oape_command`) | Code in fork only | `implementation/task-reports/<task-id>.md` per approved task |

Instructions: `{schema_root}/stage-gate/CODE_GENERATION_EVAL_PROMPT.md`  
Eval results: `openspec/changes/<change>/eval-results/code-generation-<task-id>.yaml`

## Directory layout

| Path | Purpose |
|------|---------|
| `inputs/` | Generic placeholders — replace each round |
| `refined-templates/` | Refined templates — eval workflow source of truth |
| `outputs/epic-bug-analysis/` | Current round RCA artifacts |
| `outputs/eval-generation/` | Gap analysis, patches, drafts |
| `baseline/evals/<stage>/<stage>_eval.yaml` | Cumulative eval cases per stage |
| `baseline/evals-registry.yaml` | Master index |
| `baseline/routing-learnings.md` | Bug-derived guardrails from eval loops (not workflow `agents.md`) |
| `baseline/rounds/round-N/` | Snapshot per completed loop |
| `schemas/openspec-agile-workflow/evals/` | **Forward workflow** merged stage evals (synced from `/eval-loop`) |
| `schemas/openspec-agile-workflow/stage-gate/` | Forward `/opsx-continue` eval gate instructions |
| `epic-bug-analysis/SYSTEM_PROMPT.md` | Epic Bug Analysis instructions |
| `eval-generation/SYSTEM_PROMPT.md` | Eval Generation instructions |
| `round-state.yaml` | Current round number |

## Eval Generation stages

Evals are created/updated for: **repo-assessment → constitution → plan → tasks → implementation → code-generation**.

**code-generation** evals score fork code per OAPE task during `/opsx-apply` (not markdown artifacts).

**validation.md** is refined in `evals/refined-templates/` (spec-stage eval) — not duplicated under `baseline/evals/`.
