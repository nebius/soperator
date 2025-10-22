# Helm Unit Tests for SlurmCluster Default Values

This directory contains Helm unit tests that verify the default values from kubebuilder annotations are properly applied in the slurm-cluster Helm chart.

## Test Files

- `default-values_test.yaml` - Tests individual default values from kubebuilder annotations
- `minimal-config_test.yaml` - Tests that minimal configuration with defaults renders correctly
- `optional-fields_test.yaml` - Tests that optional fields are not included when not provided

## Running Tests

To run these tests, use the `helm unittest` plugin:

```bash
# Install helm unittest plugin if not already installed
helm plugin install https://github.com/helm-unittest/helm-unittest

# Run all tests
helm unittest helm/slurm-cluster

# Run specific test file
helm unittest -f 'tests/default-values_test.yaml' helm/slurm-cluster
```

## Verified Default Values

These tests verify the following kubebuilder default values:

### SlurmClusterSpec
- `clusterType: "gpu"`
- `maintenance: "none"`
- `useDefaultAppArmorProfile: false`

### SlurmConfig
- `defMemPerNode: 1048576`
- `defCpuPerGPU: 4`
- `completeWait: 5`
- `epilog: ""`
- `prolog: ""`
- `taskPluginParam: ""`
- `maxJobCount: 20000`
- `minJobAge: 28800`
- `messageTimeout: 60`
- `topologyPlugin: "topology/tree"`
- `topologyParam: "SwitchAsNodeRank"`

### MPIConfig
- `pmixEnv: "OMPI_MCA_btl_tcp_if_include=eth0"`

### PlugStackConfig
- `pyxis.required: true`
- `pyxis.containerImageSave: "/var/cache/enroot-container-images/"`
- `ncclDebug.required: false`
- `ncclDebug.enabled: false`
- `ncclDebug.logLevel: "INFO"`
- `ncclDebug.outputToFile: true`
- `ncclDebug.outputToStdOut: false`
- `ncclDebug.outputDirectory: "/opt/soperator-outputs/nccl_logs"`
- `customPlugins: []`

### PartitionConfiguration
- `configType: "default"`
- `rawConfig: []`

### PopulateJail
- `imagePullPolicy: "IfNotPresent"`
- `appArmorProfile: "unconfined"`
- `overwrite: false`

### Worker Node
- `maxUnavailable: "20%"`
- `cgroupVersion: "v2"`
- `enableGDRCopy: false`
- `sharedMemorySize: "64Gi"`

### Accounting
- `enabled: false`
- `slurmConfig.accountingStorageTRES: "CPU,Mem,Node,VMem,Gres/gpu"`
- `slurmConfig.jobAcctGatherType: "jobacct_gather/cgroup"`
- `slurmConfig.jobAcctGatherFrequency: 30`
- `slurmConfig.priorityWeightAge: 0`
- `slurmConfig.priorityWeightFairshare: 0`
- `slurmConfig.priorityWeightQOS: 0`
- `slurmdbdConfig.*: various archive and purge settings`
- `externalDB.enabled: false`
- `externalDB.port: 3306`
- `mariadbOperator.enabled: false`
- `mariadbOperator.protectedSecret: false`
- `mariadbOperator.metrics.enabled: true`

### Login Node
- `sshdServiceNodePort: 0`

### Exporter
- `enabled: false`
- `collectionInterval: "30s"`
- `podMonitorConfig.jobLabel: "slurm-exporter"`

### REST API
- `enabled: false`
- `threadCount: 3`
- `maxConnections: 10`

### SConfigController
- `runAsUid: 1001`
- `runAsGid: 1001`
- `reconfigurePollInterval: "20s"`
- `reconfigureWaitTimeout: "1m"`

All these values match the defaults specified in the kubebuilder annotations in the Go types.
