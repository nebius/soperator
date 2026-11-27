# Soperator Flux CD chart

## HelmRelease configuration

Set `<component>.values` in the umbrella chart to configure a child chart using its native keys. Managed and Pro can continue to supply these settings through the umbrella release's own values ConfigMap.

For each child HelmRelease, the umbrella chart creates a ConfigMap named `<fullname>-<component-suffix>-values` in the HelmRelease's namespace. The suffix is the release identifier in the table below, with `notifier` for `soperator-notifier`. The ConfigMap stores the child values as YAML in `data["values.yaml"]`, or `{}` when the child has no values. Flux reads this required ConfigMap first through `spec.valuesFrom`. Child HelmReleases omit `spec.values`.

Use `<component>.valuesFrom` to supply additional ConfigMaps or Secrets in the same namespace as the HelmRelease, even when its workloads use a different `targetNamespace`. References support native Flux fields such as `valuesKey`, `optional`, and `targetPath`. Flux merges the sources in order, so the last source specifying a key wins. Helm merges maps and replaces lists. A profile's `valuesFrom` list replaces the entire list of additional references supplied by the umbrella chart. Set it to `[]` or `null` to keep only the generated ConfigMap.

For example, this profile uses the notifier defaults, the two existing optional overrides, and a Secret containing `slack.webhookUrl` under its `values.yaml` key.

```yaml
notifier:
  values:
    nameOverride: soperator-notifier
  valuesFrom:
    - kind: ConfigMap
      name: terraform-soperator-notifier
      optional: true
      valuesKey: values.yaml
    - kind: ConfigMap
      name: soperator-notifier
      optional: true
      valuesKey: values.yaml
    - kind: Secret
      name: notifier-credentials
      valuesKey: values.yaml
```

With `fullnameOverride: prod`, Flux reads `prod-notifier-values`, then the two optional ConfigMaps in the order shown, then `notifier-credentials`. Keep credentials in a referenced Secret because the umbrella chart writes `notifier.values` to a ConfigMap.

When upgrading from inline values, check for overlapping keys in existing external sources. For example, if the umbrella sets a PVC size to `100Gi` and a referenced ConfigMap sets `30Gi`, Flux previously used `100Gi` and now uses `30Gi`. To keep the installed size, update the external source or remove its size setting before upgrading.

| HelmRelease | Component |
| --- | --- |
| `ns` | `ns` |
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
| `csi-driver-nfs` | `csiDriverNfs` |
| `nfs-server` | `nfsServer` |
| `slurm-cluster-storage` | `slurmCluster.slurmClusterStorage` |
| `slurm-cluster` | `slurmCluster` |
| `nodesets` | `nodesets` |
| `soperator-activechecks` | `soperatorActiveChecks` |
| `vm-logs` | `observability.vmLogs` |
| `dcgm-exporter` | `observability.dcgmExporter` |
| `soperator-notifier` | `notifier` |
| `vm-stack` | `observability.vmStack` |
| `tsa-token-writer` | `observability.tsaToken.writer` |
| `opentelemetry-collector-logs` | `observability.opentelemetry.logs` |
| `opentelemetry-collector-jail-logs` | `observability.opentelemetry.jailLogs` |
| `opentelemetry-collector-events` | `observability.opentelemetry.events` |
| `otelcol-nccl-profiles-cm` | `observability.ncclProfiles.configMap` |
| `otelcol-nccl-profiles` | `observability.ncclProfiles.collector` |

During migration, `customConfigmaps` and `soperator.monitoringDashboards` read `overrideValues` when `values` is absent or `null`.
Move existing overrides to `values`. An explicit `values: {}` takes precedence over the legacy override.

`certManager`, `mariadbOperator`, and `backup` apply their `defaultValues` before the component's native `values`.
Cert-manager and k8up also apply `prometheusValues` when `observability.prometheusOperator.enabled` is true.
Existing nonempty `overrideValues` for these three components still replace the full map written to their generated ConfigMaps.
Kruise merges legacy overrides with its moved defaults so partial resource overrides retain the image and CRD settings.
Tailscale passes its Raw-chart resources through `tailscale.values.resources` and resolves the namespace references in that map.
The `ns` and `tsa-token-writer` ConfigMaps hold generated Bedag Raw resources. The former derives its Namespace list from the component namespaces. The latter derives the writer's Deployment and namespace-scoped RBAC from `observability.tsaToken`. Its dependency list is configured through `observability.tsaToken.writer.dependsOn`.

For `soperator` and `soperator.nodeConfigurator`, move settings from `overrideValues` to `values` in the same component.
Helm merges the new `values` maps with the chart defaults.
Move controller sizing from `soperator.values.manager.resources` to `soperator.values.controllerManager.manager.resources` before upgrading.
Set `isMariadbCrdInstalled` and `isPrometheusCrdInstalled` under `soperator.values.controllerManager.manager.env` according to the available CRDs.
Set `soperator.values.certManager.enabled` according to whether the operator uses cert-manager. The component `enabled` flags control which HelmReleases the chart creates.
An empty `rebooter.priorityClassName` explicitly sets an empty class name. Use `null` to omit the umbrella default. An additional `valuesFrom` source can override either setting.

### Storage and Slurm upgrades

Before upgrading, move `overrideValues` to `values` under `csiDriverNfs`, `nfsServer`, `slurmCluster.slurmClusterStorage`, `slurmCluster`, `nodesets`, and `soperatorActiveChecks`.
Move `nodesets.clusterName` to `nodesets.values.clusterName` as well.
Managed and Pro profiles that use the old keys need the same migration before upgrading the chart.

Helm merges `csiDriverNfs.values` with the default controller placement. Use `null` for any default placement setting that should be omitted.

Keep the existing SlurmCluster resource name when migrating a profile.
A nonempty legacy `slurmCluster.overrideValues` map without `clusterName` took the name from `valuesFrom` or the child chart's `slurm1` default.
Keep the existing name in the highest-priority source that supplies it. To use the child chart's default, set `slurmCluster.values.clusterName` to `null` and omit the key from external sources.
For a custom cluster name, set `nodesets.values.clusterName` and `soperatorActiveChecks.values.slurmClusterRefName` to the same name on enabled components.

### VictoriaLogs, DCGM, and Notifier upgrades

Move `observability.vmLogs.overrideValues` to `observability.vmLogs.values` and `notifier.overrideValues` to `notifier.values`.
The new maps merge with the defaults in this chart. Setting a defaulted key to `null` lets the child chart supply it when external sources omit it.

For VictoriaLogs profiles using the old `values` layout, move `persistentVolume`, `nodeSelector`, `affinity`, and `resources` under `observability.vmLogs.values.server`.
Change `persistentVolume.accessMode` to the `persistentVolume.accessModes` list.
An explicit `server.persistentVolume.enabled: false` now selects `emptyDir`. The old template omitted the persistence settings in that case, so the child chart could still create a PVC.
Check the existing claim before changing this setting. To keep using persistent storage, set `enabled: true` and preserve the active PVC settings.

A legacy `overrideValues` map that set only `server.retentionPeriod` left PVC settings to `valuesFrom` or the child chart.
Moving that map to `values` adds the umbrella's `30Gi` default for profiles where external sources omit the size. Carry over the active PVC settings explicitly. To keep the child chart's PVC defaults, omit the umbrella defaults as follows.

```yaml
observability:
  vmLogs:
    values:
      server:
        retentionPeriod: 7d
        persistentVolume: null
```

Move `observability.dcgmExporter.values.resources` to `observability.dcgmExporter.values.daemonSet.nvidiaDcgmExporter.resources`.
Use a YAML boolean for `validateToolkit`. The string `"false"` is truthy in the child chart and enables the toolkit validation init container.
Update the values generated by Managed and Pro as part of the chart upgrade.

For profiles that used `notifier.releaseName` as the resource name prefix, copy that name to `notifier.values.nameOverride`.
If the prefix comes from an external source, that source continues to set it. To use the child chart's default prefix and keep names such as `helm-soperator-notifier-slack-webhook`, clear the umbrella default.

```yaml
notifier:
  values:
    nameOverride: null
```

The notifier child chart checks the merged `slack.webhookUrl` when it renders its Secret.
The default `notifier.dependsOn` list waits for the namespaces and VMStack HelmReleases.

If the chart and its values ConfigMap are applied separately, suspend the umbrella HelmRelease before publishing either update and resume it after both updates are in place.

### VMStack upgrades

Move `observability.vmStack.overrideValues` to `observability.vmStack.values`. Use native `victoria-metrics-k8s-stack` keys. Managed and Pro profiles use the old kube-state-metrics and node-exporter paths and need the same changes when upgrading.

| Previous path under `observability.vmStack.values` | Native path |
| --- | --- |
| `kubeStateMetrics.resources`, `kubeStateMetrics.affinity` | `kube-state-metrics.resources`, `kube-state-metrics.affinity` |
| `kubeStateMetrics.maxScrapeSize` | `max_scrape_size` on the `http` entry of `kube-state-metrics.vmScrape.spec.endpoints` |
| `prometheusNodeExporter` | `prometheus-node-exporter` |
| `victoriaMetricsOperator` | `victoria-metrics-operator` |
| `grafanaIni` | `grafana["grafana.ini"]` |
| `dashboardNamespaces` | `grafana.sidecar.dashboards.searchNamespace` and `grafana.sidecar.datasources.searchNamespace` |

Helm replaces lists as a whole. When changing the kube-state-metrics scrape size, carry over both endpoints and their relabeling rules from this chart's defaults. Set the Grafana sidecar namespace lists and remote-write URLs explicitly when changing component namespaces or the VMSingle service name.

VMStack defaults to the local VMSingle. The public endpoint flags configure the shared token writer. Managed and Pro profiles must supply the complete `vmStack.values.vmagent.spec.remoteWrite` list, including the local URL if they use VMSingle. Set the public URL with the resolved region and project ID, the destination's relabel rules, and any write settings and labels in native vmagent values.

For Secret authentication, set `bearerTokenSecret` on the public destination. If the bundled TSA token writer maintains that Secret, add its HelmRelease to the profile's `vmStack.dependsOn` list while keeping the other prerequisites. For a host token file, set `volumes`, `volumeMounts`, and `extraArgs["remoteWrite.bearerTokenFile"]`. Keep each entry in that argument aligned with its destination and use empty entries for destinations that have their own authentication. This example uses the bundled writer for the public destination and keeps local storage.

```yaml
observability:
  vmStack:
    dependsOn:
      - name: '{{ include "soperator-fluxcd.fullname" . }}-ns'
      - name: '{{ if .Values.certManager.enabled }}{{ include "soperator-fluxcd.fullname" . }}-cert-manager{{ end }}'
      - name: '{{ include "soperator-fluxcd.fullname" . }}-victoria-metrics-operator-crds'
      - name: '{{ include "soperator-fluxcd.o11yTsaTokenWriterName" . }}'
    values:
      vmagent:
        spec:
          remoteWrite:
            - url: https://metrics.example.invalid/write
              bearerTokenSecret: {name: o11y-writer-sa-token, key: accessToken}
            - url: http://vmsingle-metrics-victoria-metrics-k8s-stack.monitoring-system.svc.cluster.local.:8428/api/v1/write
```

A nonempty legacy `overrideValues` replaced the entire umbrella configuration for VMStack. Migrating it to `values` adds defaults for omitted keys, including Grafana authentication and VMSingle storage. To keep a complete external configuration, set `observability.vmStack.values: null` and put the desired values in the external sources. Flux reads the empty generated ConfigMap first, then `terraform-victoria-metrics-operator`, then `victoria-metrics-operator`. A later source can replace just the `remoteWrite` list while inheriting the rest of the generated defaults.

### OpenTelemetry collector upgrades

Each collector passes its own native OpenTelemetry chart values through `observability.opentelemetry.<component>.values`, where the component is `logs`, `jailLogs`, or `events`. All three target `observability.opentelemetry.namespace`. Move `logs.values.jailLogs.enabled` to `jailLogs.enabled` and configure any jail-logs HelmRelease settings such as `version`, `timeout`, and `postRenderers` under `jailLogs`.

When `<component>.values.config` is nonempty, the generated ConfigMap disables the upstream Zipkin receiver and the metrics and traces pipelines for all three collectors. Jail logs also disables the Jaeger, OTLP and Prometheus receivers. These defaults are stored in [opentelemetry-disabled-defaults.yaml](files/opentelemetry-disabled-defaults.yaml) so that their YAML `null` values reach the child chart. To enable a supported receiver or pipeline, set its native configuration under `<component>.values.config` or supply it through a later `valuesFrom` entry.

Move `events.overrideValues` to `events.values`. The legacy `logs.overrideValues` applied to both log releases. Carry over the values needed by each release into `logs.values` and `jailLogs.values`, and move jail logs resource settings from `logs.values.jailLogs.resources` to `jailLogs.values.resources`.

Flux reads each collector's generated ConfigMap, then `terraform-opentelemetry-collector-<suffix>`, then `opentelemetry-collector-<suffix>`. The suffix is `logs`, `jail-logs`, or `events`. To retain a complete legacy override, set `<component>.values` to `null` and supply the chart values through those external sources. The generated base then contains only `{}`.

The child chart merges an external `config` with its own defaults. To keep only the jail logs pipeline, set the `jaeger`, `otlp`, `prometheus`, and `zipkin` entries under `config.receivers` to `null`, and do the same for `metrics` and `traces` under `config.service.pipelines`. An external source can also supply the complete collector configuration under `alternateConfig`, including the health check used by the pod probes.

All three collectors send to VictoriaLogs by default and wait for its HelmRelease when the umbrella chart installs it. To keep sending to Nebius Cloud Logging, Managed and Pro profiles must add the public exporter under `values.config.exporters.otlp` with its regional endpoint, resolved `headers.iam-container`, any additional headers, authentication, and queue settings. Include `otlp` in the relevant `config.service.pipelines` exporter list. Enable `bearertokenauth` in both `config.extensions` and `config.service.extensions`, and use `extraVolumes` and `extraVolumeMounts` to mount the Secret or hostPath containing the file named by the authenticator. When the bundled TSA token writer supplies the Secret, add `{{ include "soperator-fluxcd.o11yTsaTokenWriterName" . }}` as a dependency name in that collector's `dependsOn` list.

Move shared `batch`, `publicBatch`, and `sendingQueue` settings into each exporter's `sending_queue`. Keep the public `batch.max_size` at or below 1000 records and `queue_size` at or above `batch.min_size`. Copy cluster-specific labels into the transform statements and any environment variables they reference. Set `GOMAXPROCS` in `extraEnvs` when changing collector CPU sizing, and use the native `useGOMEMLIMIT` and `podMonitor` keys for the former Go memory limit and scrape settings.

To replace `enableFileStorage: false`, remove the `file_storage` extension and its references from `config.service.extensions`, exporter queues, and file receivers that use it. Remove the matching volume, mounts, and initialization container from their lists. To disable node logs, remove `filelog/nodelogs` from the logs receiver map and pipeline along with the node logs mount, volume, and supplemental groups. Configure jail logs polling and file retention under `config.receivers.filelog` and `config.receivers.filelog/health_checker`.

Jail logs also waits for the bundled NodeSets HelmRelease when `nodesets.enabled` is true. For separately supplied NodeSets, add their HelmRelease name and namespace if Flux manages them, or use the default list if the NodeSet resources are applied directly. Set the reference from the external HelmRelease's `metadata.name` and `metadata.namespace`. Each override replaces the entire dependency list, so include the baseline dependencies in that list.

```yaml
observability:
  opentelemetry:
    jailLogs:
      dependsOn:
        - name: '{{ include "soperator-fluxcd.fullname" . }}-ns'
        - name: '{{ if .Values.observability.vmLogs.enabled }}{{ include "soperator-fluxcd.fullname" . }}-vm-logs{{ end }}'
        - name: worker-nodesets
          namespace: flux-system
```

### NCCL profile upgrades

Set `observability.ncclProfiles.configMap.values` to native Bedag Raw values and `observability.ncclProfiles.collector.values` to native OpenTelemetry values. The Raw release creates a ConfigMap containing the complete collector configuration as a YAML string in `data.relay`. Both releases target `observability.vmStack.namespace`. The ConfigMap inherits that namespace from the Raw release. If a profile uses a custom `ncclProfiles.releaseName`, copy the installed ConfigMap name (previously `<releaseName>-agent`) into both the Raw resource's `metadata.name` and the collector's `configMap.existingName`. Set `configMap.create: false` in the collector to mount that ConfigMap.

If a Flux `postRenderers` patch selects the Raw ConfigMap with `target.namespace`, set `metadata.namespace` explicitly on the ConfigMap in `configMap.values.resources`. Flux applies the patch before Helm assigns the target namespace to a resource without one.

The Raw release depends on the namespaces release by default. The collector depends on the namespaces, VMStack, and `<fullname>-otelcol-nccl-profiles-cm` HelmReleases. When supplying `configMap.dependsOn` or `collector.dependsOn`, include the prerequisites for that release in the new list.

The former `configMapOverrideValues` and `collectorOverrideValues` replaced the entire set of umbrella values for their respective releases. Use `configMap.values` and `collector.values` for partial overrides that should inherit the umbrella chart defaults. For example, when configuring the collector to create its own ConfigMap, clear the default reference to the Raw ConfigMap and provide the desired collector configuration under `collector.values.config`.

```yaml
observability:
  ncclProfiles:
    collector:
      values:
        configMap:
          create: true
          existingName: null
```

To preserve a full legacy override, merge it over the `values.yaml` entry in the user ConfigMap and set that release's `values` to `null`. The default user ConfigMap is `opentelemetry-collector-nccl-profiles-cm` for the Raw release and `opentelemetry-collector-nccl-profiles` for the collector. Flux reads the empty generated ConfigMap, then the corresponding `terraform-` prefixed ConfigMap, then the user ConfigMap.

The shared profile runs one Deployment replica on a system node with the `Recreate` rollout strategy. To collect node-local profiles, set native values for a worker DaemonSet.

```yaml
observability:
  ncclProfiles:
    enabled: true
    collector:
      values:
        mode: daemonset
        replicaCount: null
        rollout:
          strategy: RollingUpdate
          rollingUpdate:
            maxUnavailable: 50%
        affinity:
          nodeAffinity:
            requiredDuringSchedulingIgnoredDuringExecution:
              nodeSelectorTerms:
                - matchExpressions:
                    - key: slurm.nebius.ai/nodeset
                      operator: In
                      values: [worker]
        tolerations:
          - effect: NoSchedule
            key: nvidia.com/gpu
            operator: Exists
```

If the profiling directory varies by profile, copy the default Raw `resources` list and collector `initContainers` list. Change the receiver's `include` glob in `data.relay` and the `dump_dir` assigned by `init-nccl-profiles-dump-dir` to point to the same directory under `/mnt/jail` where the Slurm jobs write their profiles.

Before upgrading profiles that supply the old `ncclProfiles.values` map, move the remaining settings to their native fields.

| Former setting | Native setting |
| --- | --- |
| `batch.timeout`, `batch.sendBatchSize`, `batch.sendBatchMaxSize` | `sending_queue.batch.flush_timeout`, `min_size`, and `max_size` on the OTLP HTTP exporter inside `data.relay` |
| `sendingQueue.queueSize`, `sendingQueue.numConsumers` | `sending_queue.queue_size` (effective size after applying the minimum batch size) and `num_consumers` on the same exporter |
| `metricsEndpoint`, `metricsExporterTimeout` | `metrics_endpoint` and `timeout` on the same exporter |
| `pollInterval`, `exportLabels` | `poll_interval` and receiver operator labels inside `data.relay` |
| `metrics` | Collector `ports.metrics`, `podMonitor`, and the telemetry feature gate under `command.extraArgs`, plus `service.telemetry.metrics` inside `data.relay` |
| `resources`, `useGoMemLimit`, `nebiusO11yAgentVersion` | Collector `resources`, `useGOMEMLIMIT`, and `image.tag`. Set `GOMAXPROCS` in `extraEnvs` when tuning CPU allocation. |

To preserve the previous queue sizing, compute `max(queueSize, batch.min_size)` where the values are generated, such as in Pro or Managed, and write the result to `sending_queue.queue_size`. For example, a queue size of `1000` and a minimum batch size of `2000` produce `queue_size: 2000` in `data.relay`.

To disable file storage, remove `file_storage` from `extensions` and `service.extensions` in `data.relay`. Remove the receiver's `storage` entry and the exporter's `sending_queue.storage`. Remove the storage volume, mount, and `init-fs` container from their respective collector lists. To enable deletion, set `delete_after_read` and `delete_after_read_min_age` on the receiver, include the `filelog.allowFileDeletion` feature gate in the collector command, and set the collector's jail mount to read-write. To disable deletion, remove both receiver settings and that feature gate, and set the collector's jail mount to read-only.

## HelmRelease dependencies

The chart declares its default dependency lists in `values.yaml`. Supply `dependsOn` on a component to replace its list.
An empty list clears the dependencies and renders `spec.dependsOn: []`. Setting a defaulted key to `null` removes it during Helm's values merge.
The template renders `spec.dependsOn` only when the merged values contain the key. Any `null` that Helm retains renders as `[]` to satisfy the Flux schema.

Use the chart's fullname helper to reference a HelmRelease in the same chart. The dependency name then follows the release name or `fullnameOverride`. For a bundled HelmRelease that can be independently disabled, guard the name with its render condition. When the condition is false, the helper skips the empty reference.

```yaml
soperator:
  dependsOn:
    - name: '{{ include "soperator-fluxcd.fullname" . }}-ns'
    - name: '{{ if .Values.soperator.kruise.enabled }}{{ include "soperator-fluxcd.fullname" . }}-kruise{{ end }}'
    - name: '{{ if .Values.certManager.enabled }}{{ include "soperator-fluxcd.fullname" . }}-cert-manager{{ end }}'
```

A literal `name` references an existing HelmRelease by its name. Flux fields such as `namespace` are forwarded as supplied.
When using an external component, supply its prerequisites and endpoints in the profile. If Flux manages that component, add its HelmRelease name and namespace to the consumer's `dependsOn` along with the other prerequisites.

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
| `observability.vmStack.namespace` | VMStack, NCCL collectors and the token writer when VMStack is enabled | `<fullname>-ns` |
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
    values:
      vmagent:
        spec:
          remoteWrite:
            - url: http://vmsingle-metrics-victoria-metrics-k8s-stack.metrics.svc.cluster.local.:8428/api/v1/write
      grafana:
        sidecar:
          dashboards:
            searchNamespace: [soperator, soperator-system, gpu-operator, monitoring-system, metrics, logs]
          datasources:
            searchNamespace: [soperator, soperator-system, gpu-operator, monitoring-system, metrics, logs]
  vmLogs:
    namespace: logs
  opentelemetry:
    namespace: logs
```

The namespace generator emits `metrics` and `logs` once each. The Grafana lists also include `monitoring-system`, where monitoring dashboard ConfigMaps are installed by default.
CRD-only releases use the same release placement in Flux across all namespace profiles.
