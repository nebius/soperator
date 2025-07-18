# Soperator ActiveCheck helm chart

This helm chart deploys ActiveCheck to soperator cluster

### To install / update:

```bash
helm upgrade -n soperator --install all-reduce-perf-nccl ./soperator-activechecks --set slurmClusterRefName=soperator
```

### To delete:

```bash
helm uninstall all-reduce-perf-nccl
```
