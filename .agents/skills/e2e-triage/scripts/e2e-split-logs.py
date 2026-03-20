#!/usr/bin/env python3
"""Downloads and splits GitHub Actions e2e-test job logs into per-step files.

Usage: e2e-split-logs.py <run-url-or-id> [repo]

Output: prints the path to a directory containing:
  steps.json          - step metadata array
  NN-step-name.log    - per-step log files

Requires: gh CLI
"""

import dataclasses
import json
import os
import re
import shutil
import subprocess
import sys
import time
from datetime import datetime, timezone

MAX_RETRIES = 5
RETRY_DELAY_SECONDS = 5


@dataclasses.dataclass
class Step:
    number: int
    name: str
    conclusion: str
    started_at: str
    started_dt: datetime

    @property
    def file(self) -> str:
        num_str = f"{self.number:02d}"
        return f"{num_str}-{slugify(self.name)}.log"


def usage():
    print(f"Usage: {sys.argv[0]} <run-url-or-id> [repo]", file=sys.stderr)
    print("  run-url-or-id: GitHub Actions run URL or numeric run ID", file=sys.stderr)
    print("  repo: owner/repo (default: nebius/soperator)", file=sys.stderr)
    sys.exit(1)


def _run_with_retries(cmd: list[str], **kwargs) -> subprocess.CompletedProcess:
    for attempt in range(MAX_RETRIES):
        result = subprocess.run(cmd, capture_output=True, **kwargs)
        if result.returncode == 0:
            return result
        if attempt < MAX_RETRIES - 1:
            print(
                f"Attempt {attempt + 1}/{MAX_RETRIES} failed, retrying in {RETRY_DELAY_SECONDS}s...",
                file=sys.stderr,
            )
            time.sleep(RETRY_DELAY_SECONDS)
    result.check_returncode()
    return result  # unreachable, but makes mypy happy


def gh_api(endpoint: str) -> str:
    result = _run_with_retries(["gh", "api", endpoint, "--paginate"], text=True)
    return result.stdout


def gh_api_raw(endpoint: str) -> bytes:
    result = _run_with_retries(["gh", "api", endpoint])
    return result.stdout


def slugify(name: str) -> str:
    """Convert step name to a file-safe slug."""
    slug = re.sub(r"[^a-zA-Z0-9 -]", "", name)
    slug = re.sub(r"\s+", "-", slug).lower()
    return slug


def parse_log_timestamp(line: str) -> datetime | None:
    """Extract and parse the ISO timestamp from a log line.

    Log lines look like: 2026-03-15T00:06:57.4345550Z Some text here
    Some lines are continuation of multiline output with no timestamp.
    """
    # Remove BOM if present
    line = line.lstrip("\ufeff")

    match = re.match(r"^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?)Z\s", line)
    if not match:
        return None

    ts_str = match.group(1)
    # Truncate fractional seconds to 6 digits (Python limit)
    ts_str = re.sub(r"(\.\d{6})\d+$", r"\1", ts_str)
    try:
        return datetime.fromisoformat(ts_str).replace(tzinfo=timezone.utc)
    except ValueError:
        return None


def parse_step_timestamp(ts_str: str) -> datetime:
    """Parse API step timestamp like '2026-03-15T00:06:57Z'."""
    return datetime.fromisoformat(ts_str.replace("Z", "+00:00"))


def steps_from_job(job: dict) -> list[Step]:
    """Extract and sort steps from a GitHub Actions job dict."""
    steps = [
        Step(
            number=s["number"],
            name=s["name"],
            conclusion=s["conclusion"],
            started_at=s["started_at"],
            started_dt=parse_step_timestamp(s["started_at"]),
        )
        for s in job["steps"]
        if s["started_at"] is not None
    ]
    steps.sort(key=lambda s: (s.started_at, s.number))
    return steps


def split_log_lines(steps: list[Step], log_lines: list[str]) -> dict[str, list[str]]:
    """Split log lines into per-step buckets using a forward-scanning state machine.

    Advances to the next step only when a ##[group] marker is seen AND the timestamp
    is >= the next step's started_at. This avoids misattributing lines when steps share
    the same second (GitHub API has only second-precision timestamps).

    Returns a dict mapping step file names to their log lines.
    """
    step_lines: dict[str, list[str]] = {s.file: [] for s in steps}
    current_idx = 0
    next_idx = 1

    for line in log_lines:
        ts = parse_log_timestamp(line)

        if ts is not None and next_idx < len(steps):
            is_group_marker = "##[group]" in line
            if is_group_marker and ts >= steps[next_idx].started_dt:
                while next_idx < len(steps) and ts >= steps[next_idx].started_dt:
                    current_idx = next_idx
                    next_idx += 1

        step_lines[steps[current_idx].file].append(line)

    return step_lines


def main():
    if len(sys.argv) < 2:
        usage()

    input_arg = sys.argv[1]
    repo = sys.argv[2] if len(sys.argv) > 2 else "nebius/soperator"

    # Parse input: accept URL or bare run ID
    url_match = re.search(r"github\.com/([^/]+/[^/]+)/actions/runs/(\d+)", input_arg)
    if url_match:
        repo = url_match.group(1)
        run_id = url_match.group(2)
    elif re.match(r"^\d+$", input_arg):
        run_id = input_arg
    else:
        print(f"Error: invalid input '{input_arg}' — expected a run URL or numeric run ID", file=sys.stderr)
        sys.exit(1)

    out_dir = f"/tmp/e2e-triage-{run_id}"
    if os.path.exists(out_dir):
        shutil.rmtree(out_dir)
    os.makedirs(out_dir)

    # Fetch job metadata
    print(f"Fetching job metadata for run {run_id}...", file=sys.stderr)
    jobs_data = json.loads(gh_api(f"repos/{repo}/actions/runs/{run_id}/jobs"))

    e2e_jobs = [j for j in jobs_data["jobs"] if j["name"] == "e2e-test"]
    if not e2e_jobs:
        available = [j["name"] for j in jobs_data["jobs"]]
        print(f"Error: no 'e2e-test' job found in run {run_id}", file=sys.stderr)
        print(f"Available jobs: {available}", file=sys.stderr)
        sys.exit(1)

    job = e2e_jobs[0]
    job_id = job["id"]
    steps = steps_from_job(job)

    # Download job log
    print("Downloading job logs...", file=sys.stderr)
    log_bytes = gh_api_raw(f"repos/{repo}/actions/jobs/{job_id}/logs")
    log_text = log_bytes.decode("utf-8", errors="replace")
    log_lines = log_text.splitlines()
    print(f"Downloaded {len(log_lines)} lines", file=sys.stderr)

    # Split logs
    print("Splitting logs into per-step files...", file=sys.stderr)
    step_lines = split_log_lines(steps, log_lines)

    # Write per-step files
    for filename, lines in step_lines.items():
        if lines:
            filepath = os.path.join(out_dir, filename)
            with open(filepath, "w") as f:
                f.write("\n".join(lines) + "\n")

    # Write steps.json
    steps_output = [
        {
            "number": s.number,
            "name": s.name,
            "conclusion": s.conclusion,
            "file": s.file,
            "line_count": len(step_lines[s.file]),
        }
        for s in steps
    ]

    with open(os.path.join(out_dir, "steps.json"), "w") as f:
        json.dump(steps_output, f, indent=2)

    # Print summary
    print("\nSteps:", file=sys.stderr)
    max_name_len = max(len(s["file"]) for s in steps_output)
    for s in steps_output:
        print(f"  {s['file']:<{max_name_len}}  {s['conclusion']:<8}  {s['line_count']} lines", file=sys.stderr)
    print(file=sys.stderr)

    # Output the directory path
    print(out_dir)


if __name__ == "__main__":
    main()
