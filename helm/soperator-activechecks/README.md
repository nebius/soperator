# Soperator ActiveCheck helm chart

This helm chart deploys ActiveCheck to soperator cluster

### To install / update:

```bash
helm upgrade --install activecheck ./soperator-activechecks -f activecheck.yaml
```
As an example we can use next `activecheck.yaml` for `k8sJobs`:
```yaml
activeCheck:
  enabled: true
  checkType: "k8sJob"
  schedule: "0 */2 * * *"    # every 2 hours
  k8sJobSpec:
    command:
      - "/bin/sh"
      - "-c"
      - "echo Hello, activecheck!"
```
and for `slurmJobs`:
```yaml
activeCheck:
  enabled: true
  checkType: "slurmJob"
  schedule: "0 */3 * * *"    # every 3 hours
  slurmJobSpec:
    sbatchScript: |
      #!/bin/bash
      #SBATCH -J simple_job
      #SBATCH --output=output.txt

      srun echo "Hello, activecheck!"
```

### To delete:

```bash
helm uninstall activecheck
```
