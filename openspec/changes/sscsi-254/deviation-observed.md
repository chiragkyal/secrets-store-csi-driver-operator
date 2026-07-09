# Deviations Observed

**Change**: sscsi-254
**Jira**: SSCSI-254

---

## Cross-cutting (all phases)

- **Tasks T1_1–T8_1 (all 19 tasks)**: `evals/code-generation_eval.yaml` contains unresolved git merge-conflict markers (`<<<<<<<`/`=======`/`>>>>>>>`), which produced an empty eval-case list for every `oape_command` filter across the entire implementation. This is a pre-existing repository/schema defect, not introduced by this change. Every task was reviewed manually against its `tasks.md` §4 acceptance criteria instead of being scored automatically, and this substitution is recorded consistently in each `eval-results/code-generation-<task-id>.yaml`. Recommend fixing the merge conflict in `evals/code-generation_eval.yaml` before the next `/opsx-apply` run on any change.
- **Tasks T7_2, T7_3, T7_4**: Full `make test-e2e` execution requires a live OpenShift cluster, which was unavailable in this environment. Only static syntax verification (`bash -n hack/e2e.sh`) plus `make verify`/`make test-unit` sanity checks were performed for these three tasks; the 12 new scenario functions themselves are unexecuted pending a live cluster run.

## Phase 1: Vendor Upstream API

- **Task T1_2**: After bumping `github.com/openshift/api` to the commit containing the new `SecretsStore` types, `make verify` failed with a version-skew build break — `github.com/openshift/client-go` (still pinned to an older version) referenced `config/v1alpha1` types that had moved/changed in the newer `openshift/api`. Resolved by performing a coordinated bump of `openshift/client-go` and `openshift/library-go` to compatible `@master` pseudo-versions in lockstep with `openshift/api`, rather than bumping `openshift/api` alone as the task payload's implementation notes suggested. This is a necessary correction to the plan, not a scope change.

## Phase 5: Unit Test Completion

- **Task T5_1**: Initial implementation assumed `storagev1.CSIDriverSpec.PodInfoOnMount` was a plain `bool`; source inspection revealed it is actually `*bool`. The test assertion was corrected before running the build, avoiding a compile failure. No production code was affected.

## Phase 7: E2E Test Scenarios

- **Task T7_3**: Discovered during implementation that `tokenRequests.type` is a one-way, CEL-enforced transition on the singleton `ClusterCSIDriver` — once set to `"Managed"` it can never revert to `"Unmanaged"` for the remainder of an e2e run. `tasks.md`/`plan.md` §3 marked `T7_2`/`T7_3`/`T7_4` `Parallel OK: Yes`, which is **not actually accurate** for `T7_3` vs. `T7_4`'s shared-singleton dependency on `Unmanaged` state. Flagged explicitly in `T7_3`'s task report as a handoff instruction for `T7_4`.
- **Task T7_4**: Resolved the `T7_3`-flagged constraint by **reordering** (not just appending to) `hack/e2e.sh`'s execution block — this task's four `Unmanaged`-dependent scenario functions (including its own concluding "post-upgrade opt-in" transition to `Managed`) were inserted before `T7_3`'s `Managed`-audience WIF block, with both blocks' comments updated to cross-reference the ordering constraint for future maintainers. This is a deliberate, intentional edit to code written in the immediately-preceding task, not an accidental merge conflict.

## Phase 8: Documentation

- **Task T8_1**: The task explicitly permitted closing as "no change needed" since `driverConfig.secretsStore`'s absence preserves identical default behavior (FR-010). Decision made to add documentation anyway, since this is a headline user-facing capability that would otherwise be undiscoverable from the README. Not a deviation from the acceptance criteria (which explicitly allow either outcome), but recorded here for completeness of the judgment-call rationale.
