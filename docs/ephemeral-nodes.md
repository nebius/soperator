# Ephemeral Nodes

Ephemeral NodeSets keep their Slurm node identities while their Kubernetes worker
pods can be powered on and off. `NodeSet.spec.replicas` defines the node range;
`NodeSetPowerState.spec.activeNodes` contains the ordinals that should have pods.
For these NodeSets, the `PodsReady` condition compares available pods with the
number of active ordinals, including zero when all nodes are powered down.

## Resume Failure Flow

Slurm calls `ResumeProgram=/opt/soperator/bin/power_resume.sh` to resume nodes.
The script uses `power-manager resume` to add their ordinals to `activeNodes`,
and the NodeSet controller updates the worker StatefulSet to create their pods.

If `slurmd` does not register within `ResumeTimeout`, Slurm marks the nodes
`DOWN+CLOUD+POWERED_DOWN` with reason `ResumeTimeout reached` and calls:

```text
ResumeFailProgram=/opt/soperator/bin/power_resume_fail.sh
```

The script uses the same power-down path as normal suspension:

```bash
/opt/soperator/bin/power-manager suspend -nodes "$1"
/opt/soperator/bin/power-manager wait-removed -nodes "$1" -timeout 180s
```

This removes the failed nodes' ordinals from `activeNodes`. The NodeSet controller
then updates the StatefulSet to remove their worker pods. `wait-removed` confirms
the power-state update; pod deletion happens asynchronously.

Soperator renders `JobRequeue=1`, allowing eligible jobs to be requeued after the
resume failure and scheduled again.

## Configuring the Timeout

Set the timeout in the `slurm-cluster` Helm values:

```yaml
slurmConfig:
  resumeTimeout: 1800
```

For a `SlurmCluster` resource, use `spec.slurmConfig.resumeTimeout`. The default
is 1800 seconds. Allow enough time for image pulls, jail population, topology
initialization when enabled, and `slurmd` registration. A timeout that is too
short can repeatedly tear down worker pods and requeue jobs.

After changing the configuration, apply it and reconfigure or restart `slurmctld`.
To inspect resume failures:

```bash
kubectl logs -n <namespace> <controller-pod> -c slurmctld | grep power_resume_fail
sinfo -N -o "%N %t %E"
kubectl get nodesetpowerstate -n <namespace>
```
