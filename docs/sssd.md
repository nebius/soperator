# SSSD support

Soperator can optionally run [SSSD](https://sssd.io/) as a sidecar for Slurm login, controller, and worker pods.

SSSD (System Security Services Daemon) is a Linux service for centralized identity management. It is commonly used to integrate Linux hosts with directory and identity systems such as:

- [LDAP](https://sssd.io/docs/ldap/ldap-introduction.html)
- [Active Directory](https://sssd.io/docs/ad/ad-provider.html)
- [FreeIPA](https://sssd.io/docs/introduction.html)

With SSSD enabled, Slurm pods can resolve remote users and groups through NSS/PAM and, when configured, use centrally managed SSH public keys.

---

## What Soperator provides

Soperator supports optional SSSD sidecars for:

- login pods
- controller pods
- worker pods

For these pods, Soperator can:

- mount an `sssd.conf` file from a Kubernetes Secret
- create a default `sssd.conf` Secret when no external Secret is provided
- mount LDAP CA certificates from a Kubernetes ConfigMap
- share SSSD sockets with the main Slurm container in the pod

This is useful when your Slurm environment needs to work with centrally managed Linux identities instead of local `/etc/passwd` entries only.

---

## Typical use cases

- LDAP-backed SSH access to login pods
- LDAP or directory-backed user resolution for `slurmctld`
- user and group resolution on worker nodes for job execution
- centrally managed SSH public keys through SSSD

---

## Configuration overview

At a high level, enabling SSSD requires:

1. Enabling the SSSD sidecar in the relevant Slurm pods.
2. Providing an `sssd.conf` Secret, or letting Soperator create a default one.
3. Optionally providing a ConfigMap with LDAP CA certificates if TLS verification is required.

In Helm-based deployments, SSSD is configured through the chart values and rendered into the `SlurmCluster` or `NodeSet` resources managed by Soperator.

---

## Notes

- SSSD is optional and disabled by default.
- Changing the SSSD config Secret or LDAP CA ConfigMap does not automatically recreate pods.
- SSH integration through `sss_ssh_authorizedkeys` is enabled only when SSSD is configured.

---

## Official SSSD documentation

- [SSSD project page](https://sssd.io/)
- [SSSD introduction](https://sssd.io/docs/introduction.html)
- [SSSD LDAP documentation](https://sssd.io/docs/ldap/ldap-introduction.html)
- [SSSD Active Directory documentation](https://sssd.io/docs/ad/ad-provider.html)
