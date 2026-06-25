#!/usr/bin/env python3
"""Render a godog cucumber JSON report as markdown for GitHub step summary."""
import json
import sys

NS_PER_SEC = 1_000_000_000

# Worst-step-wins priority. Matches godog's JUnit formatter behavior.
_STATUS_PRIORITY = {
    "failed": 4,
    "undefined": 3,
    "pending": 3,
    "ambiguous": 3,
    "skipped": 2,
    "passed": 1,
}


def fmt_s(ns):
    return f"{(ns or 0) / NS_PER_SEC:.1f}s"


def cell(s):
    return str(s).replace("|", "\\|")


def scenario_status(steps):
    if not steps:
        return "skipped"
    return max(
        (s.get("result", {}).get("status", "") for s in steps),
        key=lambda st: _STATUS_PRIORITY.get(st, 0),
    )


def render(features, out):
    out.write("### Acceptance\n")
    for feat in features:
        for scen in feat.get("elements", []):
            steps = scen.get("steps", [])
            total_ns = sum(s.get("result", {}).get("duration", 0) for s in steps)
            status = scenario_status(steps)
            out.write("\n")
            out.write(
                f"#### {cell(feat.get('name', ''))} / "
                f"{cell(scen.get('name', ''))} / {status} / {fmt_s(total_ns)}\n"
            )
            if not steps:
                continue
            out.write("\n| Step | Status | Time |\n")
            out.write("| --- | --- | --- |\n")
            for s in steps:
                keyword = s.get("keyword", "").strip()
                name = s.get("name", "")
                label = f"{keyword} {name}".strip()
                res = s.get("result", {})
                out.write(
                    f"| {cell(label)} | {res.get('status', '')} | "
                    f"{fmt_s(res.get('duration', 0))} |\n"
                )


def main():
    with open(sys.argv[1]) as f:
        features = json.load(f)
    render(features, sys.stdout)


if __name__ == "__main__":
    main()
