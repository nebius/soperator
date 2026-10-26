#!/usr/bin/env python3
"""Resolve an E2E build before claiming a profile or provisioning a cluster."""
import datetime
import io
import json
import os
import re
import subprocess
import sys
import time
import zipfile
from dataclasses import dataclass


class InvalidBuildMetadata(ValueError):
    pass


class GitHub:
    def __init__(self, repository):
        self.repository = repository

    def request(self, path, params=None, payload=None):
        command = [
            "gh", "api", "--method", "PATCH" if payload is not None else "GET",
            "-H", "Cache-Control: no-cache", f"repos/{self.repository}/{path}",
        ]
        for key, value in (params or {}).items():
            command.extend(["-F", f"{key}={value}"])
        if payload is not None:
            command.extend(["--input", "-"])
        for attempt in range(3):
            try:
                return subprocess.run(
                    command, input=json.dumps(payload).encode() if payload is not None else None,
                    capture_output=True, check=True, timeout=30,
                ).stdout
            except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
                if attempt == 2:
                    detail = (error.stderr or b"").decode(errors="replace")
                    raise RuntimeError(f"query GitHub {path}: {detail or error}") from error
                time.sleep(5 * (attempt + 1))

    def api(self, path, **params):
        return json.loads(self.request(path, params))

    def version(self, artifact):
        archive = self.request(f"actions/artifacts/{artifact['id']}/zip")
        try:
            with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
                lines = zipped.read("version.txt").decode().splitlines()
        except (zipfile.BadZipFile, KeyError, UnicodeError) as error:
            raise InvalidBuildMetadata("read version.txt from the version artifact") from error
        if (len(lines) < 3 or not re.fullmatch(r"\d+\.\d+\.\d+(?:[-+][\w.-]+)?", lines[0])
                or lines[1] not in ("true", "false") or not lines[2].strip()):
            raise InvalidBuildMetadata("version artifact must contain Soperator version, unstable flag, and NFS version; rebuild with the current build workflow")
        return dict(soperator_version=lines[0], soperator_unstable=lines[1], nfs_version=lines[2].strip())


@dataclass
class Selection:
    status: str
    reason: str = ""
    run: dict = None
    versions: dict = None


def build_runs(github, branch, sha, sleep):
    for attempt in range(3):
        response = github.api("actions/workflows/one_job.yml/runs", head_sha=sha, per_page=100)
        runs = [run for run in response["workflow_runs"]
                if run["head_sha"] == sha and run["head_branch"] == branch]
        if runs or attempt == 2:
            return sorted(runs, key=lambda run: (run["created_at"], run["id"]), reverse=True)
        sleep(5 * (attempt + 1))
    return []


def version_for_run(github, run):
    now = datetime.datetime.now(datetime.timezone.utc)
    artifacts = github.api(f"actions/runs/{run['id']}/artifacts", per_page=100)["artifacts"]
    valid = []
    for artifact in artifacts:
        if artifact["name"] != "version" or artifact.get("expired") is not False:
            continue
        try:
            expires = datetime.datetime.fromisoformat(artifact["expires_at"].replace("Z", "+00:00"))
        except (KeyError, TypeError, ValueError):
            continue
        if expires > now:
            valid.append(artifact)
    if not valid:
        raise InvalidBuildMetadata("no non-expired version artifact")
    artifact = max(valid, key=lambda item: item["expires_at"])
    return github.version(artifact)


def select_build(github, branch, commits, scheduled, *, clock=time.monotonic,
                 sleep=time.sleep, wait_seconds=3600, poll_seconds=30, on_wait=None):
    if scheduled:
        for sha in commits:
            for run in build_runs(github, branch, sha, sleep):
                if run["status"] != "completed" or run["conclusion"] != "success":
                    continue
                try:
                    return Selection("ready", run=run, versions=version_for_run(github, run))
                except InvalidBuildMetadata as error:
                    print(f"Skipping build {run['id']}: {error}", flush=True)
        raise RuntimeError(f"no valid successful build among the latest {len(commits)} commits on {branch}")

    runs = build_runs(github, branch, commits[0], sleep)
    if not runs:
        return Selection("skipped", f"No build exists for {branch} at {commits[0]}.")
    run = runs[0]
    print(f"Selected build {run['id']} at {run['head_sha']}: {run['html_url']}", flush=True)
    deadline = clock() + wait_seconds
    if run["status"] != "completed" and on_wait:
        on_wait(run)
    while run["status"] != "completed":
        remaining = deadline - clock()
        if remaining <= 0:
            return Selection("skipped", "The selected build did not complete within one hour.", run)
        print(f"Waiting for build {run['id']} ({run['status']}); {remaining:.0f}s remaining", flush=True)
        sleep(min(poll_seconds, remaining))
        if clock() >= deadline:
            return Selection("skipped", "The selected build did not complete within one hour.", run)
        run = github.api(f"actions/runs/{run['id']}")
    if clock() > deadline:
        return Selection("skipped", "The selected build did not complete within one hour.", run)
    if run["conclusion"] != "success":
        return Selection("skipped", f"The selected build concluded with {run['conclusion']}.", run)
    try:
        return Selection("ready", run=run, versions=version_for_run(github, run))
    except InvalidBuildMetadata as error:
        return Selection("skipped", f"The selected build is unusable: {error}.", run)


def git(*args):
    return subprocess.check_output(["git", *args], text=True).strip()


def main():
    from e2e_pr_comment import render_waiting

    github = GitHub(os.environ["GITHUB_REPOSITORY"])
    branch = os.environ["TARGET_BRANCH"]
    sha = os.environ.get("TARGET_SHA") or git("rev-parse", "--verify", f"refs/remotes/origin/{branch}^{{commit}}")
    scheduled = os.environ.get("IS_SCHEDULED") == "true"
    commits = git("rev-list", "--first-parent", "--max-count=15", sha).splitlines() if scheduled else [sha]

    def on_wait(run):
        comment_id = os.environ.get("COMMENT_ID")
        if comment_id:
            try:
                github.request(f"issues/comments/{comment_id}", payload={"body": render_waiting(os.environ, run)})
            except RuntimeError as error:
                print(f"Update waiting comment: {error}", file=sys.stderr)

    selection = select_build(github, branch, commits, scheduled, on_wait=on_wait)
    run = selection.run or {}
    outputs = dict(status=selection.status, reason=selection.reason,
                   run_id=run.get("id", ""), head_sha=run.get("head_sha", ""),
                   created_at=run.get("created_at", ""), html_url=run.get("html_url", ""),
                   **(selection.versions or {}))
    with open(os.environ["GITHUB_OUTPUT"], "a") as out:
        for key, value in outputs.items():
            out.write(f"{key}={str(value).replace(chr(10), ' ').replace(chr(13), ' ')}\n")
    with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as out:
        out.write(f"### Build selection: {selection.status}\n\n")
        if run:
            out.write(f"[Build {run['id']}]({run['html_url']}) at `{run['head_sha']}`\n\n")
        if selection.reason:
            out.write(selection.reason + "\n")


if __name__ == "__main__":
    main()
