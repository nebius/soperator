# Soperator Flux CD chart

## HelmRelease configuration

Set `values` on the component to pass the child chart's native keys to `spec.values` on its HelmRelease.

| HelmRelease | Component |
| --- | --- |
| `backup-config` | `backup.config` |
| `storageclasses` | `storageClasses` |
| `soperatorchecks` | `soperator.soperatorChecks` |
| `monitoring-dashboards` | `soperator.monitoringDashboards` |
| `custom-configmaps` | `customConfigmaps` |
| `prometheus-operator-crds` | `observability.prometheusOperator` |
| `mariadb-operator-crds` | `mariadbOperator.crds` |
| `victoria-metrics-operator-crds` | `observability.vmStack.crds` |

During migration, `customConfigmaps` and `soperator.monitoringDashboards` read `overrideValues` when `values` is absent or `null`.
Move existing overrides to `values`. An explicit `values: {}` takes precedence over the legacy override.

## HelmRelease dependencies

The chart declares its default dependency lists in `values.yaml`. Supply `dependsOn` on a component to replace its list.
An empty list clears the dependencies and renders `spec.dependsOn: []`. Setting a defaulted key to `null` removes it during Helm's values merge.
The template renders `spec.dependsOn` only when the merged values contain the key. Any `null` that Helm retains renders as `[]` to satisfy the Flux schema.

Use the chart's fullname helper to reference a HelmRelease in the same chart. The dependency name then follows the release name or `fullnameOverride`.

```yaml
backup:
  config:
    values:
      bucket:
        name: cluster-backups
    dependsOn:
      - name: '{{ include "soperator-fluxcd.fullname" . }}-ns'
```

A literal `name` references an existing HelmRelease by its name. Flux fields such as `namespace` are forwarded as supplied.
