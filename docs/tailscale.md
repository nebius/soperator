# Tailscale support (login pods)

Soperator can optionally enable [Tailscale](https://github.com/tailscale/tailscale) on login pods (`login-0`, `login-1`) so you can SSH to the cluster over your Tailnet.

When enabled via Terraform, Soperator:
- adds the Tailscale sidecar/init-container to login pods, and
- installs the required Kubernetes RBAC automatically.

> A Tailnet admin must still authenticate/approve the login pod devices using the short-lived URL printed in the Tailscale container logs.

---

## Enable

In your `terraform.tfvars`:

```hcl
# Whether to enable Tailscale init container on login pod.
tailscale_enabled = true
```

Apply Terraform and wait for login pods to reconcile.

---

## Authenticate login pods

Fetch the authentication URLs from the Tailscale container logs:

```bash
kubectl -n soperator logs login-0 -c tailscale
kubectl -n soperator logs login-1 -c tailscale
```

In the output, look for:

```text
To authenticate, visit:
  https://login.tailscale.com/a/...
```

A Tailnet admin must open each URL and approve/authenticate both devices (`login-0` and `login-1`) in the Tailscale admin console.

---

## SSH over Tailscale

Once approved, identify the Tailnet IP(s) for the login pods (typically `100.x.y.z`) and connect:

```bash
ssh <user>@100.x.y.z
```

---

## Troubleshooting

### Check logs

```bash
kubectl -n soperator logs login-0 -c tailscale
kubectl -n soperator logs login-1 -c tailscale
```

### Restart a login pod

The Tailscale container is intended to be stateless; restarting the pod often resolves transient issues:

```bash
kubectl -n soperator delete pod login-0
```

### Force re-authentication (if needed)

If authentication is stuck or needs to be repeated, delete the pod’s Tailscale secret (this removes stored state and should trigger a fresh auth URL):

```bash
kubectl -n soperator delete secret login-0
kubectl -n soperator delete secret login-1
```

Then re-check logs for new authentication URLs.

---

## Disable / rollback

Set in your `terraform.tfvars`:

```hcl
tailscale_enabled = false
```

Re-apply Terraform and wait for the login pods to reconcile.

---

## Notes

* This feature targets login pods (`login-0`, `login-1`) and provides SSH access over Tailnet.
* Authentication URLs are short-lived. Perform the authentication step live with the Tailnet admin.
