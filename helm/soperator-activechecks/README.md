# Soperator ActiveCheck helm chart

This helm chart deploys ActiveCheck to soperator cluster

### To install / update:

```bash
helm upgrade -n soperator --install all-reduce-perf-nccl ./soperator-activechecks --set slurmClusterRefName=soperator
```

For 4-GPU nodes such as GB300, set the Slurm GPU request used by GPU ActiveChecks:

```bash
helm upgrade -n soperator --install all-reduce-perf-nccl ./soperator-activechecks --set slurmClusterRefName=soperator --set slurmJob.gpusPerNode=4
```

### To delete:

```bash
helm uninstall all-reduce-perf-nccl
```
