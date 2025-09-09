#!/usr/bin/env python3
"""
soperator-createuser: a thin wrapper around Ubuntu/Debian `adduser` with sane defaults.

Defaults:
- Uses `--disabled-password` unless --with-password is given or caller overrides.
- Uses `--gecos ""` unless --with-gecos is given or caller overrides.
- Adds user to 'sudo' and 'docker' groups if they exist (unless disabled).
- Creates ~/.ssh and authorized_keys with secure perms, appends provided key.
- Generates an internal SSH keypair (ECDSA) unless disabled.

Interactive behavior:
- Prompts for an external SSH public key unless --without-external-ssh is set.
"""

import argparse
import grp
import os
import pwd
import shlex
import shutil
import subprocess
import sys


def ensure_root():
    if hasattr(os, "geteuid"):
        if os.geteuid() != 0:
            print("This script must be run as root.", file=sys.stderr)
            sys.exit(1)
    else:
        print("Cannot verify EUID - refusing to proceed. Run as root.", file=sys.stderr)
        sys.exit(1)


def run(cmd, *, passthrough=False, **kwargs):
    """
    Run a subprocess and surface stdout/stderr.
    If passthrough=True, inherit the parent stdio so interactive prompts are visible.
    """
    try:
        if passthrough:
            # Inherit parent's stdin/stdout/stderr so prompts display live and input works.
            res = subprocess.run(cmd, check=True, **kwargs)
            return res
        else:
            res = subprocess.run(
                cmd,
                check=True,
                text=True,
                capture_output=True,
                **kwargs,
            )
            if res.stdout:
                print(res.stdout, end="")
            if res.stderr:
                # Some tools use stderr for informational messages
                print(res.stderr, end="", file=sys.stderr)
            return res
    except subprocess.CalledProcessError as e:
        print(
            f"Error running: {' '.join(shlex.quote(str(c)) for c in cmd)}",
            file=sys.stderr,
        )
        # If we captured output, surface it; otherwise the child already printed.
        if not passthrough:
            if e.stdout:
                print(e.stdout, end="", file=sys.stderr)
            if e.stderr:
                print(e.stderr, end="", file=sys.stderr)
        sys.exit(e.returncode)


def group_exists(name: str) -> bool:
    try:
        grp.getgrnam(name)
        return True
    except KeyError:
        return False


def add_to_group(username: str, group: str):
    if not group_exists(group):
        print(f"Warning: group '{group}' does not exist; skipping.", file=sys.stderr)
        return
    print(f"Adding user '{username}' to group '{group}' ...")
    usermod = shutil.which("usermod") or "usermod"
    run([usermod, "-aG", group, username])


def parse_args():
    p = argparse.ArgumentParser(
        prog="soperator-createuser",
        formatter_class=argparse.RawTextHelpFormatter,
        description="Wrapper around `adduser` with safe defaults and SSH setup.",
    )

    # Our wrapper flags
    p.add_argument("username", help="Name of the user to create")
    p.add_argument("--with-password", action="store_true", help="Prompt for a password")
    p.add_argument("--with-gecos", action="store_true", help="Prompt for GECOS")
    p.add_argument(
        "--without-sudo",
        action="store_true",
        help="Do not add the user to the 'sudo' group",
    )
    p.add_argument(
        "--without-docker",
        action="store_true",
        help="Do not add the user to the 'docker' group",
    )
    p.add_argument(
        "--without-internal-ssh",
        action="store_true",
        help="Do not generate an internal SSH keypair (ECDSA)",
    )
    p.add_argument(
        "--without-external-ssh",
        action="store_true",
        help="Do not prompt for an external SSH public key",
    )

    # Collect remaining args intended for adduser
    args, rest = p.parse_known_args()
    return args, rest


def build_adduser_cmd(args, rest):
    """
    Ensure defaults are applied unless overridden and place USERNAME LAST.
    """
    adduser = shutil.which("adduser") or "adduser"

    # Respect caller overrides in `rest`, only inject defaults if not present
    rest_lower = [r.lower() for r in rest]

    # Default to --disabled-password unless explicitly disabled or overridden
    if not args.with_password and "--disabled-password" not in rest_lower:
        rest += ["--disabled-password"]

    # Default to --gecos "" unless explicitly disabled or overridden
    if not args.with_gecos and "--gecos" not in rest_lower:
        rest += ["--gecos", ""]

    # Username must be last for robust option parsing
    return [adduser] + rest + [args.username]


def ensure_permissions(path: str, mode: int, uid: int, gid: int, is_dir=False):
    """
    Ensure chmod/chown for a path. Creates missing files/dirs as needed.
    Does not refuse symlinks per user's request; operates on the referenced path.
    """
    if is_dir:
        os.makedirs(path, exist_ok=True)
    else:
        # Create file if missing
        if not os.path.exists(path):
            open(path, "a").close()

    os.chmod(path, mode)
    try:
        os.chown(path, uid, gid)
    except PermissionError:
        print(
            f"Warning: could not chown {path} to uid:{uid} gid:{gid}.", file=sys.stderr
        )


def append_key_line(authorized_keys: str, line: str):
    """
    Append a public key if it's not already present (simple exact-line dedup).
    """
    line = (line or "").strip()
    if not line:
        return

    # Basic sanity check, but still accept any line
    if not (
        line.startswith("ssh-")
        or line.startswith("ecdsa-")
        or line.startswith("sk-")
        or " " in line
    ):
        print(
            "Warning: provided SSH key doesn't look like a public key.", file=sys.stderr
        )

    try:
        with open(authorized_keys, "r", encoding="utf-8") as f:
            existing = {l.rstrip("\n") for l in f}
    except FileNotFoundError:
        existing = set()

    if line not in existing:
        with open(authorized_keys, "a", encoding="utf-8") as f:
            f.write(line + "\n")


def main():
    ensure_root()

    args, rest = parse_args()

    # Build and run adduser with USERNAME LAST
    cmd = build_adduser_cmd(args, rest)
    run(cmd, passthrough=True)

    # Resolve passwd entry once adduser completes
    try:
        pw = pwd.getpwnam(args.username)
    except KeyError:
        print(
            f"Error: passwd entry for '{args.username}' not found after adduser.",
            file=sys.stderr,
        )
        sys.exit(1)

    uid, gid, home_dir = pw.pw_uid, pw.pw_gid, pw.pw_dir

    # Prepare ~/.ssh and authorized_keys (no refusal on symlinks)
    ssh_dir = os.path.join(home_dir, ".ssh")
    authorized_keys = os.path.join(ssh_dir, "authorized_keys")

    ensure_permissions(ssh_dir, 0o700, uid, gid, is_dir=True)
    ensure_permissions(authorized_keys, 0o600, uid, gid, is_dir=False)

    # External key prompt unless disabled
    if not args.without_external_ssh:
        print("Enter SSH public key, or press ENTER to skip:")
        try:
            key_line = input().strip()
        except EOFError:
            key_line = ""
        if key_line:
            append_key_line(authorized_keys, key_line)

    # Group memberships
    if not args.without_sudo:
        add_to_group(args.username, "sudo")
    if not args.without_docker:
        add_to_group(args.username, "docker")

    # Internal SSH keypair (ECDSA), append its pubkey to authorized_keys
    if not args.without_internal_ssh:
        internal_key = os.path.join(ssh_dir, "id_ecdsa")
        print("Generating an internal SSH key pair (ECDSA) ...")
        ssh_keygen = shutil.which("ssh-keygen") or "ssh-keygen"
        run(
            [
                ssh_keygen,
                "-t",
                "ecdsa",
                "-f",
                internal_key,
                "-N",
                "",
                "-C",
                f"{args.username}@soperator-internal",
            ]
        )
        for p in (internal_key, internal_key + ".pub"):
            try:
                os.chown(p, uid, gid)
            except PermissionError:
                print(
                    f"Warning: could not chown {p} to {args.username}.", file=sys.stderr
                )
        try:
            with open(internal_key + ".pub", "r", encoding="utf-8") as pubf:
                append_key_line(authorized_keys, pubf.read().strip())
        except FileNotFoundError:
            print(
                "Warning: internal public key not found; skipping append.",
                file=sys.stderr,
            )

    # Final info
    added_groups = []
    if not args.without_sudo and group_exists("sudo"):
        added_groups.append("sudo")
    if not args.without_docker and group_exists("docker"):
        added_groups.append("docker")

    if added_groups:
        groups_str = ", ".join(added_groups)
        print(
            f"User '{args.username}' created. Home: {home_dir}. Added to groups: {groups_str}."
        )
    else:
        print(f"User '{args.username}' created. Home: {home_dir}.")


if __name__ == "__main__":
    main()
