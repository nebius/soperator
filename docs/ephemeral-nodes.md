# Ephemeral Nodes

Ephemeral nodes are Slurm worker nodes whose Slurm identity exists even when the
corresponding Kubernetes pod is not running. Soperator renders these nodes in
`slurm.conf` with `State=CLOUD`, which lets Slurm keep them as schedulable cloud
capacity and ask Soperator to power them on only when work needs them.

This follows Slurm's power saving model:
[Slurm Power Saving Guide](https://slurm.schedmd.com/power_save.html).

## Resources

`NodeSet` describes the worker group:

- `spec.replicas` is the maximum node range rendered into `slurm.conf`.
- `spec.ephemeralNodes: true` enables ephemeral behavior for that group.
- `spec.initialNumberEphemeralNodes` seeds the first active ordinals when the
  `NodeSetPowerState` is created.

`NodeSetPowerState` is the runtime power state for one `NodeSet`:

- It is named the same as the `NodeSet`.
- `spec.nodeSetRef` points back to the `NodeSet`.
- `spec.activeNodes` contains the powered-on ordinals, for example
  `[0, 3, 5]` for `worker-gpu-0`, `worker-gpu-3`, and `worker-gpu-5`.

The NodeSet controller owns creation and status reconciliation of
`NodeSetPowerState`. The `power-manager` binary owns changes to
`spec.activeNodes`.

For ephemeral NodeSets, Soperator renders the worker StatefulSet from
`activeNodes`: active ordinals get pods, inactive ordinals are put into
OpenKruise `reserveOrdinals`, and an empty `activeNodes` list means zero worker
pods.

## Resume Flow

When a pending Slurm job needs powered-down ephemeral nodes, Slurm moves those
nodes into power-up states and calls:

```text
ResumeProgram=/opt/soperator/bin/power_resume.sh
```

The script receives a Slurm hostlist, such as `worker-gpu-[0-3]`, and runs:

```bash
/opt/soperator/bin/power-manager resume -nodes "$1"
```

`power-manager` parses the hostlist into NodeSet names and ordinals, skips
non-ephemeral NodeSets, and adds those ordinals to
`NodeSetPowerState.spec.activeNodes`.

That update wakes the NodeSet controller. It updates the worker StatefulSet so
Kubernetes creates pods for the requested ordinals. Each pod waits for the
controller and, when enabled, topology data before starting `slurmd`. When
`slurmd` registers, Slurm can move the node through `POWER_UP` / configuring
toward usable node states and then start the job.

## Suspend Flow

Slurm calls:

```text
SuspendProgram=/opt/soperator/bin/power_suspend.sh
```

The script runs:

```bash
/opt/soperator/bin/power-manager suspend -nodes "$1"
```

`power-manager` removes the requested ordinals from
`NodeSetPowerState.spec.activeNodes`. The NodeSet controller then updates the
StatefulSet so Kubernetes removes the corresponding pods.

Automatic suspend is controlled by:

```yaml
slurmConfig:
  suspendTime: 600
```

`SuspendTime` is the number of idle seconds after which Slurm considers
ephemeral `State=CLOUD` nodes eligible for power down. A negative value disables
automatic power down. Automatic resume for jobs is independent of
`SuspendTime`.

After changing Slurm config, apply the change and reconfigure or restart
`slurmctld`.

## Manual Power Control

You can exercise the same Slurm power path manually from a Slurm login or
controller shell:

```bash
scontrol power up worker-gpu-[0-1]
scontrol power down worker-gpu-[0-1] Reason="manual suspend test"
scontrol power down asap worker-gpu-2 Reason="finish current job, then suspend"
scontrol power down force worker-gpu-3 Reason="force suspend test"
```

Equivalent state updates are also supported by Slurm:

```bash
scontrol update nodename=worker-gpu-0 state=power_up
scontrol update nodename=worker-gpu-0 state=power_down reason="manual suspend test"
```

Useful checks:

```bash
sinfo -N -o "%N %t %E"
kubectl get nodeset,nodesetpowerstate -n <namespace>
kubectl get pods -n <namespace> -l app.kubernetes.io/component=nodeset
```

If a node is powered up in Slurm but no pod appears, check the
`power_resume`/`power-manager` logs in the controller pod and verify the
controller service account can update `NodeSetPowerState` resources.
