# Worker SSH adoption with pam_slurm_adopt

Soperator can require a non-root user to own a running Slurm job on a worker before accepting SSH and can place the
accepted SSH session in that job's `extern` step. The session then inherits the job's cgroup CPU, memory, and device
constraints and is terminated when Slurm removes the job.

This feature applies only to worker nodes. Login nodes do not run `slurmd` and do not receive the module or its PAM
policy.

## Configuration

For the `slurm-cluster` Helm chart:

```yaml
pamSlurmAdopt:
  enabled: true
  actionUnknown: newest
  exemptUsers: []
  exemptGroups: []
```

The equivalent `SlurmCluster` API configuration is:

```yaml
spec:
  pamSlurmAdopt:
    enabled: true
    actionUnknown: newest
    exemptUsers: []
    exemptGroups: []
```

All fields are optional. Omitting `pamSlurmAdopt` or setting `enabled: false` leaves worker SSH behavior unchanged.
`actionUnknown` defaults to `newest` and accepts:

- `newest`: when Slurm cannot identify the originating job and the user owns more than one eligible job on the worker,
  adopt the session into the newest job.
- `deny`: reject the ambiguous SSH session.

`exemptUsers` and `exemptGroups` are empty by default. An exempt identity bypasses `pam_slurm_adopt`: it does not need a
job, but its session is also not placed in a job cgroup. Use exemptions only for accounts that need worker access
independent of jobs.

Identity names are cluster-specific. Configure the exact names resolved on workers through NSS, SSSD, or LDAP; do not
assume a built-in Nebius service user or administrator group. Verify candidate values with commands such as
`getent passwd <user>`, `id <user>`, and `getent group <group>` on a worker. Names such as `user@domain`,
`DOMAIN\user`, machine accounts ending in `$`, and group names containing spaces are accepted. Empty values,
surrounding whitespace, control characters, and duplicates are rejected.

## Generated runtime configuration

When enabled, Soperator:

- adds `ulimit_pam_adopt` to `LaunchParameters`; `PrologFlags=contain` is already part of the generated Slurm
  configuration;
- mounts a worker-only PAM account policy and optional user and group exemption files;
- activates `pam_slurm_adopt.so` as the last account rule used by worker `sshd`;
- keeps the existing `pam_soperator_jail.so` session hook, which enters the shared jail mount namespace;
- starts the worker only after checking the PAM modules, ordering, required options, `sshd` configuration, and the
  absence of an active `pam_systemd` session rule.

The adoption policy uses `join_container=false`. It moves the SSH session into the Slurm extern-step cgroup without
joining a job-created namespace, so the existing worker jail, Docker, Enroot, and Slurm task namespace design remains
unchanged.

If `customSlurmConfig` overrides `PrologFlags` or `LaunchParameters`, the webhook requires those effective overrides to
retain `contain` and `ulimit_pam_adopt` respectively while adoption is enabled.

## Safe rollout

Enabling or disabling the feature changes the worker pod template. Replacing a worker pod stops jobs on that worker
unless the NodeSet uses a coordinated rollout. Use `spec.updateStrategy: slurmAwareRollingUpdate` with an appropriate
`maxUnavailable`; see [Worker and node rollout](worker-node-rollout.md).

A job created before `PrologFlags=contain` took effect has no extern step and cannot accept an adopted session. For an
upgrade from a deployment that did not already use `Contain`, roll out in this order:

1. Upgrade Soperator and the worker image while leaving `pamSlurmAdopt.enabled: false`.
2. Confirm the effective configuration contains `PrologFlags=CONTAIN` and let jobs that predate that configuration
   finish, or arrange a cutover for long-running jobs.
3. Enable `pamSlurmAdopt`. This adds `ulimit_pam_adopt` and starts the worker rollout that activates the PAM policy.
4. Remove processes that were already orphaned before enablement; the module only handles new SSH sessions.
5. Validate with a regular user. Root bypasses `pam_slurm_adopt`.

On installations where all current jobs were created with `PrologFlags=contain`, the first two steps are already
satisfied. Check rather than assume when upgrading an older or customized cluster.

## Validation

Use a non-exempt, non-root account:

1. SSH to a worker where the user has no job and confirm access is rejected.
2. Start a job for the user on that worker and SSH again.
3. Confirm `/proc/self/cgroup` contains a `step_extern` component.
4. For a one-GPU allocation, confirm `nvidia-smi` exposes only one GPU.
5. Cancel or preempt the job and confirm the SSH connection ends.

The acceptance suite contains the same end-to-end check in `features/pam_slurm_adopt.feature`. It temporarily enables
the feature, performs a worker rollout, uses a dedicated test user, and restores the original API value afterward. The
scenario is tagged `@unstable` because it rolls the workers twice. Run it explicitly on a disposable test cluster with
unstable scenarios enabled.

```sh
go run ./e2e/cmd/acceptance \
  --kubectl-context <context> \
  --run-unstable \
  --scenario features/pam_slurm_adopt.feature:4
```
