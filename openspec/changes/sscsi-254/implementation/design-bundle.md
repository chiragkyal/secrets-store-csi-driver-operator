# Design Bundle — Task T4_1

**Change:** sscsi-254
**Task:** T4_1 — Implement rotation-args `DaemonSetHookFunc`
**Assigned Agent:** ControllerLogic_Agent
**Phase:** Phase 4: DaemonSet Rotation Hook

## Repo-assessment / reusable asset (structural precedent)

> `csidrivernodeservicecontroller.WithCABundleDaemonSetHook` (`vendor/.../csidrivernodeservicecontroller/helpers.go:32-75`) — closure-captured lister/informer, mutates `daemonSet.Spec.Template.Spec`, returns error on failure. Exact pattern to follow.
> `DaemonSetHookFunc` signature: `func(*opv1.OperatorSpec, *appsv1.DaemonSet) error` — synchronous, context-free (confirmed in `T2_2`'s design bundle).

## Current hardcoded args (`assets/node.yaml:37-48`)

```yaml
args:
  - "--endpoint=$(CSI_ENDPOINT)"
  - "--logtostderr"
  - "--v=${LOG_LEVEL}"
  - "--nodeid=$(KUBE_NODE_NAME)"
  - "--provider-volume=/var/run/secrets-store-csi-providers"
  - "--additional-provider-volume-paths=/etc/kubernetes/secrets-store-csi-providers"
  - "--metrics-addr=:8095"
  - "--enable-secret-rotation=true"
  - "--rotation-poll-interval=2m"
  - "--provider-health-check=false"
  - "--provider-health-check-interval=2m"
```
Container name: `csi-driver`.

## Task T4_1 Payload (from tasks.md §4)

- **Objective:** Implement a new `DaemonSetHookFunc` that sets `--enable-secret-rotation=`/`--rotation-poll-interval=` on the `csi-driver` container based on `T2_1`'s resolved rotation config.
- **Target file(s):** New file under `pkg/operator/`, structurally following `WithCABundleDaemonSetHook`.
- **Non-goals / forbidden edits:** Do not modify the existing `WithCABundleDaemonSetHook` registration or `assets/cabundle_cm.yaml` — purely additive hook.
- **Implementation notes:** Find/replace args by `--flag=` prefix match on the `csi-driver` container; when config is unset, preserve the existing hardcoded defaults (`true`, `2m`-equivalent) baked into `assets/node.yaml:45-46` today (FR-010, upgrade safety).
- **Acceptance criteria:** Traces to FR-001, FR-002. Verified by `T4_3` (not this task, but a smoke test is added here per mandatory verification).
- **Downstream handoff:** A working hook function ready for registration in `T4_2`.

## Design decision: interval string format

`RotationPollIntervalSeconds` (`T2_1`) is an `int32` seconds count. Rather than using Go's `time.Duration.String()` (which renders exactly 120s as `"2m0s"`, not the hardcoded `"2m"` in `assets/node.yaml`), this hook formats the interval as `"<N>s"` (e.g. `"120s"`) — a valid Go duration string the driver's flag parser accepts identically to `"2m"` (both parse to the same 120-second `time.Duration`). This is a **functionally identical, not byte-identical** default per FR-010 — the resolved behavior is unchanged, but the literal flag string differs from today's hardcoded `"2m"`. Documented here for `T5_1`'s regression-parity tests, which should assert parsed-duration/resolved-config equivalence rather than exact string match.

## Execution approach

New file `pkg/operator/daemonsethook.go`: `withSecretsStoreRotationDaemonSetHook(lister) csidrivernodeservicecontroller.DaemonSetHookFunc`, plus small `findContainer`/`setArgPrefix` helpers. Not yet registered in `starter.go` (that's `T4_2`).
