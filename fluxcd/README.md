# Project Structure

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
  This directory contains environment-specific and cluster-specific configuration. For the initial implementation, we plan to have two environments and three types of clusters:

  **Environments**:
  - `nebius-cloud` - Environment for the Nebius Cloud provider.
  - `local` -  Environment for local development (e.g., using kind, orbstack or Minikube).

Each subfolder typically has its own `kustomization.yaml`. The structure can be extended as needed to adapt to more complex setups or additional environments.

## File and Directory Layout

Below is an example of the project layout under the `fluxcd` directory:

```
fluxcd
├── enviroment
│   ├── nebius-cloud
│   │   ├── kustomization.yaml
│   │   └── git-repository.yaml
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

To deploy a specific cluster configuration, use [Kustomize](https://kustomize.io/) and apply it with `kubectl`. For example, to deploy the `nebius-cloud` configuration:

```bash
flux create
kustomize build --load-restrictor LoadRestrictionsNone fluxcd/enviroment/nebius-cloud/bootstrap | kubectl apply -f -
```

In this command:
- `--load-restrictor LoadRestrictionsNone` allows Kustomize load files from outside their root.
- `fluxcd/enviroment/nebius-cloud` points to the directory containing the `kustomization.yaml` for that specific environment and cluster type.

### Hierarchical Rendering with Kustomize

Kustomize may require you to “walk up” the directory structure to gather configurations. For instance, a `kustomization.yaml` in `fluxcd/clusters/nebius-cloud/` might reference:
1. Its parent environment-level `kustomization.yaml` (such as `fluxcd/clusters/nebius-cloud/base/`).
2. Which in turn could reference specific `base` components in `fluxcd/base/...`.

This hierarchical approach allows for maximum flexibility; configurations for local development may not be suitable for production, so each environment can override only what is needed while reusing a common base.
