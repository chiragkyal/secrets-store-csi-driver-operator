# Sample ClusterCSIDriver YAML Verification — T6_2

**File:** `config/manifests/stable/sscsi-sample-clustercsidriver-secretsstore.yaml`  
**Branch commit:** `ea278f2d` (T6_2)  
**Verified:** 2026-07-10

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| Valid YAML | PASS — `yaml.safe_load` succeeds |
| ConsoleYAMLSample pattern | PASS — matches `sscsi-sample-secretproviderclass-*.yaml` structure |
| Custom rotation | PASS — `secretRotation.type: Custom`, `minimumRefreshAge: 120` |
| Managed tokenRequests | PASS — `tokenRequests.type: Managed` with AWS + Azure audiences |
| No edits to existing samples | PASS — new file only |

## Sample Content Summary

- **Target resource:** `ClusterCSIDriver` (`secrets-store.csi.k8s.io`)
- **Rotation:** Custom 120s interval
- **WIF:** Managed audiences `sts.amazonaws.com` (3600s) and `api://AzureADTokenExchange`

No file changes required — sample already committed on branch.
