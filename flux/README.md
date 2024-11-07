# Bootsrap for local development

## k8s single node installation

### k3s
see https://docs.k3s.io/quick-start

### kind
see https://kind.sigs.k8s.io/docs/user/quick-start/

## Flux installation
see https://fluxcd.io/flux/installation/

## Kustomize instalation
see https://kubectl.docs.kubernetes.io/installation/kustomize/

## Deploy flux
flux install

## Deploy flux-kustomization

kustomize build --load-restrictor LoadRestrictionsNone flux/clusters/single-node/init | kubectl apply -f -

# Example of ubuntu bootstrap

## Install k3s
```curl -sfL https://get.k3s.io | sh -```
and reboot node

## Install fluxcd
```
curl -s https://fluxcd.io/install.sh | sudo bash
. <(flux completion bash)
flux install
```
## Install kustomize
```
curl -s "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"  | bash
sudo cp /home/${USER}/kustomize /usr/local/bin/
```

### Install slurm

```
https://github.com/nebius/soperator.git
git checkout dev
kustomize build --load-restrictor LoadRestrictionsNone flux/clusters/single-node/init | kubectl apply -f -
```
