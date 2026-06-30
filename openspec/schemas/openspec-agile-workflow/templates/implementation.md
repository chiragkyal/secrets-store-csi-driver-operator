# Implementation Phase Log

**Change**: [CHANGE_NAME]
**Jira**: [JIRA_KEY]
**Fork**: [FORK_REPO_URL]
**Branch**: [FEATURE_BRANCH]
**Started**: [DATE]

Append one section per **approved task** during `/opsx:apply`. Each approved task
also gets a full report at `implementation/task-reports/[TASK_ID].md`.

---

## Task: [TASK_ID] — [TASK_TITLE]

**Phase**: [PHASE_NAME]
**Status**: Approved
**Agent**: [ASSIGNED_AGENT]
**OAPE Command**: [api-generate | api-generate-tests | api-implement | e2e-generate | manual]
**Task report**: [implementation/task-reports/TASK_ID.md]

### OAPE Commands Executed

| Command | Args | Outcome |
|---------|------|---------|
| /oape:… | … | Success |

### Code Generation Eval

| Metric | Value |
|--------|-------|
| Overall score | [N]% |
| Cases pass | [N]/[M] |
| Refinement rounds | [0–2] |
| Eval results | eval-results/code-generation-[TASK_ID].yaml |

### Files Touched

- `relative/path/to/file`

### Test Results

| Test | Result | Notes |
|------|--------|-------|
| make test | PASSED | |

### Deviations

- [description — omit section when none]

---

## Phase Log Notes

- Per-task flow: OAPE → verify → code evals → refine code → **user code approval** → task report.
- Design bundle: `implementation/design-bundle.md` (regenerated per task, scoped to one Task ID).
- On reject: REVISION FEEDBACK in design bundle; re-run the current task only.
