# AppArmor

Soperator selects AppArmor profiles for login sshd and worker slurmd containers.
Provisioning must load named profiles on every node where these containers may run.

## Configuration

Profiles are configured per container:

- Login: `SlurmCluster.spec.slurmNodes.login.sshd.appArmorProfile`
- Workers: `NodeSet.spec.slurmd.security.appArmorProfile`

Both default to `unconfined` in the CRDs and Helm charts. A profile name or
`localhost/<profile-name>` selects a profile already loaded on the node.

The [Nebius Terraform recipe](https://github.com/nebius/nebius-solutions-library/tree/main/soperator)
loads `soperator-default` and sets these two fields to that name when
`use_default_apparmor_profile` is `true` (the recipe default). When disabled,
it sets both fields to `unconfined`. Other containers keep their own profiles.

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
