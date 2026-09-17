# Project Structure

> **⚠️ DEPRECATION NOTICE**: The Kustomize-based bootstrap process is no longer supported. Please use the `soperator-fluxcd-bootstrap` Helm chart for deploying FluxCD resources. See the [Bootstrap with Helm Chart](#bootstrap-with-helm-chart) section below.

The project structure is based on the [hierarchical repository pattern](https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/concepts/hierarchical-repo) from Anthos. We use [FluxCD](https://fluxcd.io/) to manage configurations, with a division into **`enviroment`** and **`base`** directories:

- **`base`**  
  This directory contains foundational manifests and components used throughout the solution, such as Helm charts for nodeconfigurator, soperator, soperatorchecks, slurm-cluster-storage, slurm-cluster, and other components that the `soperator` depends on (for example, installing observability agents, the MariaDB operator, or the security-profile-operator).  
  - Typically contains at least two files:
    1. **`kustomization.yaml`** – Required for Kustomize rendering.  
    2. **`resources.yaml`** – Contains the primary manifests to be deployed.  

  Most often, in these manifests, we will use `HelmRepository` and `HelmRelease` objects. In `HelmRelease`, we may optionally specify a `valuesFrom` field referencing a ConfigMap to fine-tune resource configuration depending on the cluster’s size or environment, for example:

  ```yaml
  valuesFrom:
    - kind: ConfigMap
      name: operator
      valuesKey: values.yaml
      optional: true
  ```

- **`enviroment`**  
  This directory contains environment-specific and cluster-specific configuration. We support the following environments:

  **Environments**:
  - `nebius-cloud` - Environment for the Nebius Cloud provider.
  - `local` - Local development environment for kind clusters.

  **Clusters** (for nebius-cloud):
  - `prod` – Production environment with stable configuration.
  - `dev` – Development environment with unstable/latest configuration.

  **Local Environment**:
  - `local` – Minimal configuration for local kind cluster development.

Each subfolder typically has its own `kustomization.yaml`. The structure can be extended as needed to adapt to more complex setups or additional environments.

## File and Directory Layout

Below is an example of the project layout under the `fluxcd` directory:

```
fluxcd
├── environment
│   ├── nebius-cloud
│   │   ├── prod
│   │   │   └── kustomization.yaml
│   │   ├── dev
│   │   │   └── kustomization.yaml
│   │   └── base
│   │       └── kustomization.yaml
│   └── local
│       ├── kustomization.yaml
│       ├── namespace.yaml
│       ├── helmrepository.yaml
│       ├── helmrelease.yaml
│       └── values.yaml
│
└── base
    └── soperator-fluxcd
        ├── kustomization.yaml
        └── resources.yaml
```

### Notes on the Structure

- `base/fluxcd/` and `base/soperstor/` each have their own `kustomization.yaml` and `resources.yaml`.  
- `clusters/*` typically contain specific overrides or references to the `base` directory so that each environment or cluster type can customize which components are deployed and how they are configured.  
- `git-repository.yaml` (in each cluster subfolder) usually defines the source of the Git repository that FluxCD will watch.

## Deployment

### Legacy Deployment (Deprecated)

> **⚠️ DEPRECATED**: The Kustomize-based bootstrap is no longer maintained. Use the Helm chart method above instead.

<details>
<summary>Click to expand legacy deployment instructions</summary>

### Production/Dev Deployment (Nebius Cloud)

To deploy a specific cluster configuration, use [Kustomize](https://kustomize.io/) and apply it with `kubectl`. For example, to deploy the `nebius-cloud-dev` configuration:

```bash
flux install
kustomize build --load-restrictor LoadRestrictionsNone fluxcd/environment/nebius-cloud/dev/bootstrap | kubectl apply -f -
```

### Local Development Deployment (Kind)

For local development with kind clusters, use the simplified Makefile target:

```bash
# Create kind cluster
make kind-create

# Deploy soperator via Flux CD
make deploy-flux
```

This will:
1. Install Flux CD to your cluster
2. Deploy the local environment configuration from `fluxcd/environment/local/`
3. Create a ConfigMap with minimal values for local development
4. Deploy soperator-fluxcd HelmRelease

To manually deploy without Makefile:

```bash
flux install
kustomize build fluxcd/environment/local | kubectl apply -f -
```

To check deployment status:

```bash
kubectl get helmreleases -n flux-system
kubectl get helmrepositories -n flux-system
flux get all -n flux-system
```

To undeploy:

```bash
make undeploy-flux
```

In these commands:
- `--load-restrictor LoadRestrictionsNone` allows Kustomize load files from outside their root (for nebius-cloud deployments).
- `fluxcd/environment/local` points to the directory containing the local development configuration.

### Hierarchical Rendering with Kustomize

Kustomize may require you to “walk up” the directory structure to gather configurations. For instance, a `kustomization.yaml` in `fluxcd/clusters/nebius-cloud/dev/` might reference:
1. Its parent environment-level `kustomization.yaml` (such as `fluxcd/clusters/nebius-cloud/base/`).
2. Which in turn could reference specific `base` components in `fluxcd/base/...`.

This hierarchical approach allows for maximum flexibility; configurations for local development may not be suitable for production, so each environment can override only what is needed while reusing a common base.

</details>

## Migration from Kustomize to Helm Chart

If you are currently using the Kustomize-based bootstrap, migrate to the Helm chart method:

1. **Remove old Kustomize resources**:
   ```bash
   kubectl delete -k fluxcd/environment/<your-env>/bootstrap
   ```

2. **Create ConfigMap with your values**:
   ```bash
   kubectl create configmap soperator-fluxcd-values \
     -n flux-system \
     --from-file=values.yaml=<path-to-your-values>
   ```

3. **Install the bootstrap Helm chart**:
   ```bash
   helm install soperator-bootstrap \
     oci://cr.eu-north1.nebius.cloud/soperator/soperator-fluxcd-bootstrap \
     --version <version> \
     --namespace flux-system
   ```

4. **Verify the deployment**:
   ```bash
   kubectl get helmrepository -n flux-system
   kubectl get helmrelease -n flux-system
   flux get all -n flux-system
   ```
