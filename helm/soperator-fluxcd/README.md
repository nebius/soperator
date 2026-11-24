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
| `tailscale` | `tailscale` |
| `cert-manager` | `certManager` |
| `mariadb-operator` | `mariadbOperator` |
| `k8up` | `backup` |
| `kruise` | `soperator.kruise` |
| `soperator` | `soperator` |
| `nodeconfigurator` | `soperator.nodeConfigurator` |

During migration, `customConfigmaps` and `soperator.monitoringDashboards` read `overrideValues` when `values` is absent or `null`.
Move existing overrides to `values`. An explicit `values: {}` takes precedence over the legacy override.

`certManager`, `mariadbOperator`, and `backup` apply their `defaultValues` before the component's native `values`.
Cert-manager and k8up also apply `prometheusValues` when `observability.prometheusOperator.enabled` is true.
Existing nonempty `overrideValues` for these three components still replace the full inline map.
Kruise merges legacy overrides with its moved defaults so partial resource overrides retain the image and CRD settings.
Tailscale passes its Raw-chart resources through `tailscale.values.resources` and resolves the namespace references in that map.

For `soperator` and `soperator.nodeConfigurator`, move settings from `overrideValues` to `values` in the same component.
Helm merges the new `values` maps with the chart defaults.
Move controller sizing from `soperator.values.manager.resources` to `soperator.values.controllerManager.manager.resources` before upgrading.
Set `isMariadbCrdInstalled` and `isPrometheusCrdInstalled` under `soperator.values.controllerManager.manager.env` according to the available CRDs.
Set `soperator.values.certManager.enabled` according to whether the operator uses cert-manager. The component `enabled` flags control which HelmReleases the chart creates.
An empty `rebooter.priorityClassName` explicitly sets an empty class name. Use `null` to remove the inline key and allow a value from `valuesFrom`.

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
A dependency whose Helm name expression resolves to an empty string is omitted from the rendered list.

## Namespaces

The `ns` block configures the `<fullname>-ns` Raw HelmRelease. That HelmRelease renders one `Namespace` per distinct declaration.
The Slurm and operator namespaces come from `slurmCluster.namespace` and `soperator.namespace`.
Optional components declare their namespaces when enabled, following the same parent flags as their HelmReleases.
The chart uses the merged Helm values. An empty or null name is skipped. Flux uses the HelmRelease's own namespace when its `targetNamespace` is omitted.
Changing the `targetNamespace` of an installed HelmRelease causes Flux to uninstall and reinstall it.

| Declaration | Resources using the namespace | Namespace renderer |
| --- | --- | --- |
| `slurmCluster.namespace` | SlurmCluster, cluster storage, backup configuration and the Kruise Helm target | `<fullname>-ns` |
| `nodesets.namespace` | NodeSets | `<fullname>-ns` |
| `soperatorActiveChecks.namespace` | ActiveChecks | `<fullname>-ns` |
| `soperator.namespace` | Operator, soperatorchecks and nodeconfigurator | `<fullname>-ns` |
| `soperator.monitoringDashboards.namespace` | Dashboard ConfigMaps | `<fullname>-ns` |
| `customConfigmaps.namespace` | Custom ConfigMaps | `<fullname>-ns` |
| `tailscale.namespace` | Tailscale Role and RoleBinding | `<fullname>-ns` |
| `certManager.namespace` | Cert-manager and its default leader election namespace | `<fullname>-ns` |
| `mariadbOperator.namespace` | MariaDB operator and webhook | `<fullname>-ns` |
| `backup.namespace` | k8up operator | `<fullname>-ns` |
| `nfsServer.namespace` | NFS server | `<fullname>-ns` |
| `csiDriverNfs.namespace` | NFS CSI driver | `<fullname>-ns` |
| `storageClasses.namespace` | StorageClasses Helm target | `<fullname>-ns` |
| `notifier.namespace` | Notifier | `<fullname>-ns` |
| `observability.vmStack.namespace` | VMStack, its local remote-write endpoint, NCCL collectors and the token writer when VMStack is enabled | `<fullname>-ns` |
| `observability.vmLogs.namespace` | VictoriaLogs | `<fullname>-ns` |
| `observability.opentelemetry.namespace` | OpenTelemetry collectors and the token writer when only OpenTelemetry is enabled | `<fullname>-ns` |
| `observability.prometheusOperator.namespace` | Monitoring namespace declaration for the cluster-scoped Prometheus CRD release | `<fullname>-ns` |
| `observability.dcgmExporter.namespace` | DCGM exporter | `<fullname>-ns` |
| Kruise child chart defaults | `kruise-system` and `kruise-daemon-config` | Kruise chart |

Configure NodeSets, ActiveChecks, Tailscale RBAC and the ConfigMaps mounted by Slurm pods in the same namespace as their SlurmCluster.
Custom ConfigMaps use the Helm release namespace by default. Set the child chart's `namespace` in `customConfigmaps.values` or the `custom-configmaps` values ConfigMap to override it.

VictoriaLogs and the OpenTelemetry collectors share a namespace when using the default local logs endpoint. The VMStack namespace can be separate.

```yaml
observability:
  vmStack:
    namespace: metrics
  vmLogs:
    namespace: logs
  opentelemetry:
    namespace: logs
```

The namespace generator emits `metrics` and `logs` once each. Grafana discovery and token writer references use the component namespaces.
CRD-only releases use the same release placement in Flux across all namespace profiles.
