# AppArmor

Soperator uses the node-local `soperator-default` profile for login sshd and
worker slurmd containers when `useDefaultAppArmorProfile` is enabled. Provisioning
must load the profile on every node where these containers may run.

## Configuration

`useDefaultAppArmorProfile` defaults to `false` in the Helm chart and SlurmCluster
CR. The [Nebius Terraform recipe](https://github.com/nebius/nebius-solution-library/tree/main/soperator)
defaults `use_default_apparmor_profile` to `true`, enabling both profile loading
and its selection in workloads.

To select a custom profile, disable `useDefaultAppArmorProfile` and set
`SlurmCluster.spec.slurmNodes.login.sshd.appArmorProfile` or
`NodeSet.spec.slurmd.security.appArmorProfile` to `localhost/<profile-name>`.
The custom profile must also be loaded on the nodes.

## Node requirements

The recipe's [cloud-init template](https://github.com/nebius/nebius-solution-library/blob/main/soperator/modules/k8s/templates/cloud_init.yaml.tftpl)
loads the embedded profile with `apparmor_parser --replace` on every boot and
verifies enforce mode. Node images must provide AppArmor, securityfs, the parser,
and the `tunables/global` and `abstractions/base` includes before `bootcmd` runs.
For other provisioning systems, use the template as a reference and load the
profile on every boot.

## Verification

On a node, check `/sys/kernel/security/apparmor/profiles` for
`soperator-default (enforce)`. Inside a login or worker container, check
`/proc/1/attr/current`. For loading errors, inspect `cloud-init status --long`
and `/var/log/cloud-init-output.log` on the node.
