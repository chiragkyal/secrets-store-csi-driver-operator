# Design Bundle — Task T2_2

**Change:** sscsi-254
**Task:** T2_2 — Wire informer/typed-client access in `starter.go`
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 2: Shared Read Path

## Constitution excerpts (binding)

> **Principle I:** Any new operator capability MUST be expressed as either a new CSIControllerSet hook, a new static asset, or a new informer — never a separate reconciler loop.
> **Performance guideline:** Scope informers to the specific namespaces/resources needed... avoid cluster-wide watches where unnecessary. (`ClusterCSIDriver` is a single named cluster-scoped singleton — a targeted informer for it is consistent with this guidance.)

## Plan / Open Question 2 (from plan.md §8)

> Exact mechanism: dedicated typed informer/lister vs. direct typed-client `Get` call. Default assumption: dedicated informer/lister for consistency with this operator's existing informer-driven design — but prototype before committing since this pattern is not yet proven in this specific operator.

## Decisive constraint discovered during T2_1

Both consumer signatures this helper must eventually feed are **synchronous and context-free**:
- `resourceapply.AssetFunc`: `func(name string) ([]byte, error)`
- `csidrivernodeservicecontroller.DaemonSetHookFunc`: `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error`

Neither accepts a `context.Context`, which rules out a live typed-client `Get` call (that would need a context and would hit the API server on every reconcile/hook invocation). This confirms Open Question 2's default assumption: **a lister-backed informer is the correct mechanism**, matching every existing hook in this codebase (`WithCABundleDaemonSetHook` takes a `configMapInformer corev1.ConfigMapInformer` and reads via `.Lister()`, never a live client `Get`).

## Repo-assessment excerpts (reusable pattern)

> `WithCABundleDaemonSetHook(configMapNamespace, configMapName, configMapInformer)` — closure-captured lister, read via `.Lister().ConfigMaps(ns).Get(name)`. This is the exact structural precedent for how the new `ClusterCSIDriverLister` should be threaded into consumers in T3_1/T4_1.

## Task T2_2 Payload (from tasks.md §4)

- **Objective:** Instantiate and start whatever informer/lister `T2_1`'s helper needs, inside `RunOperator`.
- **Target file(s):** `pkg/operator/starter.go` (informer-construction block, lines 40-71; informer-start block, lines 118-121).
- **Non-goals / forbidden edits:** Do not remove or alter the existing generic operator client — this adds an additional, separate access path.
- **Acceptance criteria:** The new informer is started alongside the existing three. `make verify && make test-unit` passes.
- **Downstream handoff:** A working, started read path that `T3_1`, `T3_2`, and `T4_1` can call without additional wiring.

## Execution approach

Add a typed `ClusterCSIDriver` informer (via `github.com/openshift/client-go/operator/{clientset/versioned,informers/externalversions}`), start it alongside the existing informers, and — since Go requires every declared variable to be used within its own task's compiling diff, and to give this wiring genuine, testable value now rather than leaving an inert unused variable — add a small startup-only goroutine that waits for the informer's cache to sync and logs the resolved `secretsStore` config once (using T2_1's `ResolveSecretsStoreConfig`). This is a real, useful diagnostic (mirrors the existing `klog.Info("Starting the informers")` pattern) and does not preempt T3_1/T4_1's actual mutation logic (CSIDriver `AssetFunc`, DaemonSet hook), which will consume the same informer/lister via their own constructor parameters in the next tasks.
