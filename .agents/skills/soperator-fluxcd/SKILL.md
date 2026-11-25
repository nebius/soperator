---
name: soperator-fluxcd
description: Change or review the Soperator Flux umbrella chart in helm/soperator-fluxcd, including HelmRelease dependencies, native child chart values, namespaces, and upgrade profiles.
---

# Soperator Flux umbrella chart

Read the repository instructions and `helm/soperator-fluxcd/README.md`. Identify the component's values path, the child chart and version, and the profiles that consume it before planning changes.

## Native chart values

- Put defaults under `<component>.values` using the child chart's actual keys. Pass them directly to `spec.values`. Keep installation metadata such as `enabled`, `namespace`, and `dependsOn` on the component.
- Preserve the order of existing `valuesFrom` entries. We should not use inline values. Helm merges native maps and replaces lists. Check Managed and Pro generators for the affected keys and coordinate upgrades when their profiles use them.
- Set endpoints, child chart namespaces, and integration flags in values or in the profile that knows the deployment context.

## HelmRelease dependencies

Declare the default list in `<component>.dependsOn` in `values.yaml`. Use `soperator-fluxcd.dependsOn` in the template and `soperator-fluxcd.fullname` for umbrella-managed names. For an independently optional HelmRelease, guard its name with the target's render condition. Include any target parent flag that the consumer does not already guarantee. Use a direct name when the releases always render together. The NFS entry in `slurmCluster.dependsOn` follows this pattern.

```yaml
- name: '{{ if .Values.nfsServer.enabled }}{{ include "soperator-fluxcd.fullname" . }}-nfs-server{{ end }}'
```

The helper renders names in the umbrella context, skips empty names, and forwards fields such as `namespace` and `readyExpr`. An override replaces the list in full, so carry over the other prerequisites. `[]` emits an empty list and `null` removes a defaulted key during Helm coalescing.

For external Flux releases, supply their actual `metadata.name` and `metadata.namespace` in the profile's list.

## Verification

- Compare default and all-enabled renders. Toggle independently optional prerequisites and check that no default edge points to a missing HelmRelease. Exercise an override with `fullnameOverride`, an external reference, `[]`, and `null` when changing the dependency contract.
- Render the affected child charts when changing native values and compare resources that rely on them. The umbrella API check validates generated resources, including HelmRelease objects. A separate child chart render shows the workloads a HelmRelease would install.
- After chart edits, run `make sync-version-from-scratch`, `make helmtest`, `make test-helm-api`, `make test`, and `make lint` as applicable. Use the Kubernetes version configured in `Makefile` for API validation. Report checks limited by missing cached dependencies when working offline.
