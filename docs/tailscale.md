# Tailscale support (login pods)

Soperator can optionally enable [Tailscale](https://github.com/tailscale/tailscale) on Slurm login pods so you can SSH to login nodes securely over your Tailnet.

High level steps:
1. Apply RBAC so Tailscale can store state in Kubernetes Secrets.
2. Add a Tailscale container to login pods via the `SlurmCluster` resource.
3. A Tailnet admin authenticates each login pod device using the short-lived URL printed in the Tailscale container logs.

> The number of login pods is configurable. Authenticate every login pod you want reachable via Tailnet.

> Note: This is install-tool agnostic. If you deploy Soperator via Helm/Flux, you still apply the RBAC and update the `SlurmCluster` resource the same way.

---

## 1) Apply RBAC
> Note: Some deployment templates may restrict RBAC to specific Secret names (for example `login-0`/`login-1`). If you run more than two login pods, ensure RBAC allows `get/update/patch` on Secrets for all login pods (or remove `resourceNames` restrictions).

Create a Role/RoleBinding in the `soperator` namespace.

```bash
kubectl apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  namespace: soperator
  name: tailscale
rules:
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "create", "update", "patch"]
- apiGroups: [""]
  resources: ["events"]
  verbs: ["get", "create", "patch"]
EOF
```

Bind the Role to the ServiceAccount used by login pods. Many deployments use `default` (adjust if yours differs):

```bash
kubectl apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: tailscale
  namespace: soperator
subjects:
- kind: ServiceAccount
  name: default
  namespace: soperator
roleRef:
  kind: Role
  name: tailscale
  apiGroup: rbac.authorization.k8s.io
EOF
```

---

## 2) Add Tailscale to login pods

Patch the `SlurmCluster` to add a Tailscale container under:
`spec.slurmNodes.login.customInitContainers[]`

```bash
kubectl -n soperator patch SlurmCluster soperator \
  --type='json' \
  -p='[{
    "op":"add",
    "path":"/spec/slurmNodes/login/customInitContainers/-",
    "value":{
      "name":"tailscale",
      "image":"ghcr.io/tailscale/tailscale:latest",
      "imagePullPolicy":"Always",
      "restartPolicy":"Always",
      "securityContext":{"privileged":true},
      "env":[
        {"name":"POD_NAME","valueFrom":{"fieldRef":{"fieldPath":"metadata.name"}}},
        {"name":"POD_UID","valueFrom":{"fieldRef":{"fieldPath":"metadata.uid"}}},
        {"name":"TS_DEBUG_FIREWALL_MODE","value":"auto"},
        {"name":"TS_KUBE_SECRET","valueFrom":{"fieldRef":{"fieldPath":"metadata.name"}}},
        {"name":"TS_USERSPACE","value":"false"}
      ]
    }
  }]'
```

Wait for login pods to restart:

```bash
kubectl -n soperator get pods -l app.kubernetes.io/component=login -w
```
---

## 3) Authenticate login pods

List login pods:

```bash
kubectl -n soperator get pods -l app.kubernetes.io/component=login
```

For each login pod, fetch the auth URL from logs:

```bash
kubectl -n soperator logs <login-pod-name> -c tailscale
```

Look for:

```text
To authenticate, visit:
  https://login.tailscale.com/a/...
```

A Tailnet admin must open the URL and approve/authenticate the device.

Tip: print auth URLs for all login pods:

```bash
for p in $(kubectl -n soperator get pods -l app.kubernetes.io/component=login -o jsonpath='{.items[*].metadata.name}'); do
  echo "=== $p ==="
  kubectl -n soperator logs "$p" -c tailscale | tail -n 50
  echo
done
```

---

## SSH over Tailscale

Once approved, SSH to the Tailnet IP of a login pod:

```bash
ssh <user>@100.x.y.z
```

---

## Disable / rollback

1. Remove the `tailscale` entry from `SlurmCluster.spec.slurmNodes.login.customInitContainers` (edit the `SlurmCluster` and remove the container, then apply).
2. Delete RBAC:

```bash
kubectl -n soperator delete rolebinding tailscale
kubectl -n soperator delete role tailscale
```

---

## Notes

* This enables Tailnet connectivity to login pods only (not cluster-wide subnet routing).
* Auth URLs are short-lived; do authentication live with a Tailnet admin.
