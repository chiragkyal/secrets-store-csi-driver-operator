# Code-generation stage samples

Optional reference when authoring `code-generation_eval.yaml` cases during `/eval-loop`.

## What to derive from a feature bundle

| Input | Look for |
|-------|----------|
| `05-repo-prs.md` | API marker mistakes, controller anti-patterns, missing tests |
| `bugs/*.md` | Regressions fixable by code eval assertions |
| `issue-taxonomy.json` | PAT-* patterns tagged implementation / code |

## oape_command tagging

| Command | Typical code under test |
|---------|-------------------------|
| `api-generate` | `api/**`, `*_types.go`, CRD markers |
| `api-generate-tests` | `test/apis/**`, `.testsuite.yaml` |
| `api-implement` | `pkg/controller/**`, reconcilers |
| `e2e-generate` | `test/e2e/**`, Ginkgo or bash e2e |
| `manual` | `bindata/`, `config/`, RBAC, OLM bundles |
| `any` | Cross-cutting (effective-go, constitution) |

## Example case shape

See `evals/stages/code-generation/eval-spec.yaml` → `example`.
