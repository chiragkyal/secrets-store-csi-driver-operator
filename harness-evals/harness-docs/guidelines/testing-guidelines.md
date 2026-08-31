# Testing Guidelines

## Test Structure

- Use table-driven tests with named test cases as the default pattern. Each test case is a struct with a descriptive `name` field.
- Run subtests with `t.Run(tc.name, func(t *testing.T) { ... })` so each case appears as a separate subtest in output and can be run individually.
- Name test cases descriptively to explain the scenario being tested, e.g., `"operator state is Removed when ClusterCSIDriver does not exist"`.

## Test Organization

- Place unit tests in `_test.go` files alongside the code they test in the same package.
- The main test file is `pkg/operator/starter_test.go` — match this pattern for new packages.
- E2E tests are invoked via `hack/e2e.sh` and the Ginkgo suites under `test/e2e/` and `test/e2e/azure/` (see "Ginkgo E2E Suites" below).

## Fakes and Mocks

- Use `library-go`'s `FakeOperatorClient` (`v1helpers.NewFakeOperatorClientWithObjectMeta`) for unit testing operator logic without a real cluster.
- Use the `FakeOperatorClient` pattern to set up operator spec, status, and object metadata for testing sync behavior.
- Do not use third-party mocking frameworks — prefer hand-written fakes and the standard library's testing utilities.

## Test Assertions

- Use standard `if` checks with `t.Errorf` or `t.Fatalf` — no assertion libraries.
- Use `t.Fatalf` when a failure makes the rest of the test meaningless (e.g., setup failure, nil pointer).
- Use `t.Errorf` when subsequent assertions may still provide useful diagnostic information.
- Compare expected values explicitly: `if got != want { t.Fatalf("expected sync state to be %v, got %v", want, got) }`.

## Test Data and Fixtures

- Operator manifests (YAML assets) are embedded in the binary via `//go:embed` in `assets/assets.go` and loaded via `assets.ReadFile()`. Tests that depend on these assets can use the same loading mechanism.
- Test data for operator state is constructed inline in test cases using struct literals — no external fixture files for unit tests.

## Makefile Test Targets

- `make test-unit` — runs unit tests via `go test`.
- `make test-e2e` — runs `hack/e2e.sh`, then the cloud-agnostic Ginkgo suite in `test/e2e`. Pass `RUN_AZURE_E2E=true` to also run the real-Azure WIF suite in `test/e2e/azure` (used by the `operator-e2e-azure` CI job).
- `make test-e2e-azure-wif` — alias for `make test-e2e RUN_AZURE_E2E=true`.
- `make verify` — runs code verification (formatting, vetting, Go version checks).
- `make test` — runs `test-unit` (the default test target).
- Run `make verify` before submitting changes to catch formatting and vet issues.

## E2E Testing

- E2E tests require a running OpenShift cluster with the operator and driver already deployed, and are executed via `hack/e2e.sh` plus the Ginkgo suites below.
- `hack/e2e.sh` handles its own test setup, execution, and teardown including artifact collection. It creates an ephemeral namespace (`secrets-store-test-ns-<random>`) and cleans it up via `test_teardown`. It validates: CSIDriver resource existence, provider pod readiness, SecretProviderClass creation, and secret volume mounting.
- E2E tests are run in CI via Prow jobs — they are not expected to run locally in most development workflows.

### Ginkgo E2E Suites

Two Ginkgo v2 + Gomega suites cover the configurable secret rotation and workload identity federation (WIF) feature (`driverConfig.secretsStore`):

- **`test/e2e` (`make test-e2e`)** — cloud-agnostic. Asserts only on the `storage.k8s.io/v1` `CSIDriver` object and the driver's node DaemonSet, covering the EP's rotation and tokenRequests scenarios (defaults, `Custom`/`None` rotation, `Managed`/`Unmanaged`/omitted tokenRequests, multi-audience, upgrade preservation).
- **`test/e2e/azure` (`make test-e2e RUN_AZURE_E2E=true`)** — real Azure. Gated by `RUN_AZURE_E2E=true` (skipped otherwise). Creates a real Key Vault, secret, user-assigned managed identity, and federated identity credential, installs the real Azure provider via the upstream pinned `provider-azure-installer.yaml` release manifest (provider only — the operator already manages the CSI driver), configures `driverConfig.secretsStore.tokenRequests` (replacing the manual `oc patch csidriver` workaround), and runs a Ginkgo port of upstream `azure.bats`: inline CSI volume mount, Kubernetes secret sync (including owner references), namespaced `SecretProviderClass` scope (positive and negative), multiple `SecretProviderClass` volumes, and real Key Vault secret rotation. Requires an Azure Workload Identity (manual OIDC) cluster, the `oc` CLI in `$PATH`, network access to the Azure Resource Manager and Key Vault endpoints (and to GitHub to fetch the pinned provider manifest), and Azure service principal credentials at `$CLUSTER_PROFILE_DIR/osServicePrincipal.json` (the standard OpenShift CI convention). All Azure resource CRUD (Key Vault, managed identity, federated credential) goes through the Azure SDK for Go (`github.com/Azure/azure-sdk-for-go/sdk/...`) rather than shelling out to the `az` CLI; `oc` (provider install, OpenShift/Kubernetes objects) still shells out via `os/exec`. Run via the `operator-e2e-azure` Prow job (`openshift-e2e-azure-csi-secrets-store-azure` workflow).

**`tokenRequests.type: Managed` is a one-way, irreversible transition**, enforced by a CEL rule on `ClusterCSIDriver` (see `vendor/github.com/openshift/api/operator/v1/types_csi_cluster_driver.go`) — once set, it can never be reverted to `Unmanaged`, even by clearing `driverConfig` entirely.

- In `test/e2e`, the specs that transition `tokenRequests.type` to `Managed` are isolated in an `Ordered` container and gated behind `RUN_IRREVERSIBLE_E2E=true` (skipped by default), since this suite may run against a persistent developer or CI cluster via plain `make test-e2e`. Set `RUN_IRREVERSIBLE_E2E=true` only if you're certain the cluster's `ClusterCSIDriver` singleton being permanently switched to `Managed` tokenRequests is acceptable.
- `test/e2e/azure` always transitions `tokenRequests.type` to `Managed` when `RUN_AZURE_E2E=true` (that is the feature under test). Its target CI job (`operator-e2e-azure`) provisions an ephemeral, per-run WIF cluster that is destroyed afterward.

## Code Verification

- `make verify` checks:
  - `go vet ./...` for common Go issues.
  - `gofmt` for code formatting.
  - Go version consistency across `go.mod` and Dockerfile.
- Run `make verify` locally before pushing to catch issues that would fail in CI.
- Verification checks are implemented in vendored `build-machinery-go` makefiles.
- Fix formatting violations with `make update-gofmt`.

## CI Integration

- Tests run in OpenShift CI (Prow) on every pull request.
- The CI configuration lives in the `openshift/release` repository, not in this repo.
- CI runs `make test-unit` and `make verify` on every PR.
- E2E tests run as periodic or pre-submit Prow jobs against a real cluster.
- CI builds use FIPS-compliant build flags (`CGO_ENABLED=1 GOEXPERIMENT=strictfipsruntime` with `-tags strictfipsruntime,openssl`).

## Adding New Tests

1. Place the test file next to the source file it tests, using the same package name.
2. Use table-driven tests with descriptive `name` fields.
3. Use `t.Fatalf` for fatal assertions and `t.Errorf` for non-fatal assertions; do not import external assertion libraries.
4. Use `library-go` fakes (`v1helpers.NewFakeOperatorClientWithObjectMeta`) for operator client mocking.
5. If the test needs new assets, add the YAML to `assets/` and update the embed directive if a new subdirectory is introduced.
6. Run `make verify && make test-unit` locally before submitting a PR.

## References

- [SECRETS_STORE_TESTING.md](../SECRETS_STORE_TESTING.md) — Full testing guide, including component-specific test scenarios
- [Component Architecture](../architecture/components.md) — What to test
- [Error Handling Guidelines](./error-handling-guidelines.md) — Error path testing conventions
- [Platform Testing Practices](https://github.com/openshift/enhancements/tree/master/ai-docs/) — Cross-repo patterns
