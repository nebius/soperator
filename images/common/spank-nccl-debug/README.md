# NCCL debug SPANK plugin

## Deployment

### Disable Soperator

```shell
kubectl -n soperator-system scale deployment soperator-controller-manager --replicas 0
```

### ConfigMap creation

```bash
kubectl -n soperator create configmap spanknccldebug --from-file ./build/spanknccldebug.so
```

### Volumes

For `worker`/`login` StatefulSet volumes:

```yaml
- name: spanknccldebug
  configMap:
    name: spanknccldebug
```

### Volume mounts

For `worker`/`login` StatefulSets volume mounts:

```yaml
- name: spanknccldebug
  mountPath: /usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so
  subPath: spanknccldebug.so
  readOnly: true
```

### PlugStack conf

For `soperator-slurm-configs` ConfigMap:

```yaml
plugstack.conf: >-
    required chroot.so /mnt/jail

    required spank_pyxis.so runtime_path=/run/pyxis execute_entrypoint=0
    container_scope=global sbatch_support=1
    container_image_save=/var/cache/enroot-container-images/

    optional /usr/lib/x86_64-linux-gnu/slurm/spanknccldebug.so
```

### Restart pods

```shell
kubectl -n soperator delete pod {worker,login}-0 --wait=false
```
or (not reliable)
```shell
kubectl-kruise -n soperator rollout restart statefulset.apps.kruise.io/{worker,login}
```

