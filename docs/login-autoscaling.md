# Login Autoscaling

Available in Soperator 5.0.0 and later. Login autoscaling is disabled by default.

## Overview

Login workloads can become CPU-bound as users run interactive commands, build
software, or prepare jobs. Soperator can create a Kubernetes HorizontalPodAutoscaler
(HPA) to add login pods as CPU utilization increases, up to a configured maximum.
New pods provide capacity for new SSH connections through the login Service.
Existing sessions and their processes remain on their original pods; they are not
moved to the new pods. This feature does not scale Slurm worker pods.

The HPA measures average CPU utilization of the `sshd` containers relative to their
CPU requests, not the utilization of the entire Kubernetes node or other containers.
For example, a target of 70% with a request of `3000m` corresponds to an average of
2.1 CPU cores per SSHD container. See Kubernetes documentation on
[container resource metrics](https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/#container-resource-metrics).

## Requirements

- Install matching Soperator operator and CRD chart versions that support login autoscaling.
- Provide Kubernetes resource metrics through `metrics.k8s.io`, typically using Metrics Server.
- Configure a positive SSHD CPU request in `slurmNodes.login.sshd.resources.cpu`.
- Provide enough Kubernetes capacity for additional login pods, or configure node
  autoscaling separately. The HPA scales pods, not Kubernetes nodes. Additional pods
  can use existing nodes; if no suitable capacity is available, they remain Pending.

## Configuration

Apply these settings through the configuration source that manages your cluster
(for example, Helm values managed by Terraform or GitOps), so they are not overwritten
by the next deployment.

### Helm

Merge this fragment into your existing `slurm-cluster` chart values, retaining the
rest of your cluster configuration:

```yaml
slurmNodes:
  login:
    autoscaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 5
      targetCPUUtilizationPercentage: 70
```

### SlurmCluster

For a directly managed `SlurmCluster`, merge the equivalent fragment into its
existing manifest. This is not a complete cluster manifest:

```yaml
spec:
  slurmNodes:
    login:
      autoscaling:
        enabled: true
        minReplicas: 2
        maxReplicas: 5
        targetCPUUtilizationPercentage: 70
```

`minReplicas` and `maxReplicas` are required and must be at least 1;
`maxReplicas` must not be below `minReplicas` when autoscaling is enabled.
`targetCPUUtilizationPercentage` accepts values from 1 to 100 and defaults to 70.

While autoscaling is enabled, the HPA manages the replica count within these bounds.
Changing `login.size` does not resize the workload until autoscaling is disabled.
Omitting `autoscaling`, or setting `enabled: false`, leaves the fixed `login.size`
in control.

### Terraform

For deployments using `nebius-solutions-library`, configure login autoscaling in
[`soperator/installations/example/terraform.tfvars`](https://github.com/nebius/nebius-solutions-library/blob/e4e0b7e1dcdf2d00a1c44f027c20fd8edec4e43a/soperator/installations/example/terraform.tfvars).
Use a repository revision that includes support for `slurm_nodeset_login.autoscaling`.
Add this block inside the existing `slurm_nodeset_login` object, preserving its
`size`, `resource`, and `boot_disk` settings:

```hcl
autoscaling = {
  enabled                           = true
  min_size                          = 2
  max_size                          = 5
  target_cpu_utilization_percentage = 70
}
```

These settings configure login-pod autoscaling and map to the HPA bounds and CPU
target shown above. They do not configure Kubernetes node-group scaling limits.
The existing `size` becomes the fixed login pod count again when autoscaling is disabled.

## Scale-down and maintenance

Automatic scale-down in response to falling CPU utilization is disabled. Removing
CPU pressure does not remove the additional login pods, avoiding session disruption
caused by load-based scale-down.

To reduce the count deliberately, either:

- Set `autoscaling.enabled: false` and set `login.size` to the desired fixed count.
  Soperator removes the HPA and reconciles the workload to that count.
- Lower `autoscaling.maxReplicas` below the current count while keeping it at least
  `minReplicas`. HPA still enforces its maximum bound even with load-based scale-down
  disabled.

Coordinate reductions with users: sessions and processes on removed pods are
interrupted. The autoscaling configuration does not let you choose which pods to
remove or select them based on active SSH sessions. Deleting an individual pod does
not reduce the desired replica count; the StatefulSet recreates it.

During maintenance modes that scale login pods to zero, Soperator removes the HPA.
When maintenance ends and autoscaling is enabled, it restores `minReplicas` and
recreates the HPA.

## Check status

For a cluster named `soperator` in namespace `soperator`, using the default workload
name prefix:

```bash
kubectl -n soperator get hpa soperator-login
kubectl -n soperator describe hpa soperator-login
kubectl -n soperator get statefulsets.apps.kruise.io soperator-login
kubectl -n soperator get pods -l app.kubernetes.io/instance=soperator,app.kubernetes.io/component=login -o wide
kubectl -n soperator top pods -l app.kubernetes.io/instance=soperator,app.kubernetes.io/component=login --containers
```

Replace the namespace, cluster label, and workload name for your installation.
The HPA has the same name as the login StatefulSet.

Existing clusters may retain legacy unprefixed workload names: `login` for the
StatefulSet and HPA, and `login-0`, `login-1`, etc. for pods. For these clusters,
replace `soperator-login` with `login` in the commands above; the label selectors
remain unchanged for a cluster named `soperator`.

If the HPA reports unknown CPU utilization, inspect its conditions and events and
check that resource metrics and SSHD CPU requests are available. If additional pods
are Pending, inspect their scheduling events with `kubectl describe pod` and check
available capacity, node selectors, affinity, and taints. A working HPA does not by
itself guarantee that Kubernetes can schedule the requested pods.
