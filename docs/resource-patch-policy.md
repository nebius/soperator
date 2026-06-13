# ResourcePatchPolicy

ResourcePatchPolicy is an experimental, opt-in escape hatch that lets you patch
the Kubernetes resources soperator generates for `SlurmCluster`, `NodeSet` and
`NodeConfigurator` objects — without forking the operator or fighting the
reconciliation loop.

Patches are applied to the in-memory desired object before it is submitted to
the API server, so the operator re-applies them on every reconciliation. There
is no race with external controllers.

This feature is permanently `v1alpha1`: the operator does not guarantee the
naming scheme or structure of generated resources across releases, so a policy
that works today may need updating after an upgrade.

## Enabling the feature

The feature is toggled like a controller through `controllersEnabled` and is
off by default. Enable it in the operator's Helm values:

```yaml
controllerManager:
  manager:
    controllersEnabled:
      resourcepatchpolicy: true
```

This is wired into the `SLURM_OPERATOR_CONTROLLERS` mechanism, so it can also be
toggled directly via that environment variable (or the `--controllers` flag),
e.g. `SLURM_OPERATOR_CONTROLLERS="*,resourcepatchpolicy"` /
`--controllers="cluster,nodeset,nodeconfigurator,topology,resourcepatchpolicy"`.

Enabling it both registers the policy status controller and turns on in-memory
patching inside the cluster, nodeset and nodeconfigurator reconcilers.

## Anatomy of a policy

```yaml
apiVersion: slurm.nebius.ai/v1alpha1
kind: ResourcePatchPolicy
metadata:
  name: node-configurator-host-network
  namespace: soperator-system
spec:
  targetRef:
    group: slurm.nebius.ai
    kind: NodeConfigurator      # or SlurmCluster / NodeSet
    name: soperator-node-configurator
  priority: 10                  # lower numbers applied first; default 0
  type: JSONPatch               # or JSONMergePatch
  patches:
    # The DaemonSet is named "<NodeConfigurator.name>-ds".
    - resourceRef:
        kind: DaemonSet
        name: "soperator-node-configurator-ds"   # exact resource name
      jsonPatch:
        - op: add
          path: "/spec/template/spec/hostNetwork"
          value: true
```

- `targetRef` binds the policy to exactly one parent object. Its namespace
  defaults to the policy's namespace.
- `type` selects RFC 6902 JSON Patch (`JSONPatch`) or RFC 7386 JSON Merge Patch
  (`JSONMergePatch`).
- `resourceRef.kind` plus `resourceRef.name` (exact resource name) selects
  which generated resources a patch applies to. An empty `name` matches every
  resource of that kind. `apiVersion` may be set to disambiguate.
- In JSON Pointers, `/` inside a key is escaped as `~1` and `~` as `~0`
  (e.g. `sidecar.istio.io/inject` → `sidecar.istio.io~1inject`).

## Ordering and conflicts

Policies that target the same resource are applied in ascending `priority`
order; ties are broken alphabetically by `namespace/name`. A later patch that
writes the same field wins.

If a single patch entry fails (bad path, malformed payload, protected-field
violation) it is skipped and the remaining entries still apply. A `Warning`
event is recorded on the parent object.

## Protected fields

The following mutations are rejected to keep reconciliation working:

| Field                      | Reason                          |
| -------------------------- | ------------------------------- |
| `metadata.name`            | identity cannot change          |
| `metadata.namespace`       | identity cannot change          |
| `metadata.ownerReferences` | breaks garbage collection       |
| `spec.selector`            | immutable on workload resources |

## Status

The ResourcePatchPolicy controller validates each policy statically and reports
an `Accepted` condition:

```yaml
status:
  conditions:
    - type: Accepted
      status: "True"
      reason: Accepted
      message: Policy passed static validation
```

An invalid policy (e.g. a `JSONPatch` entry with no operations) gets
`status: "False"` and `reason: Invalid` with a descriptive message.
