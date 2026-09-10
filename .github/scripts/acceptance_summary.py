#!/usr/bin/env python3
"""Render a godog cucumber JSON report as Markdown."""
import argparse
import json
import re
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

_MARKDOWN_SPECIAL = re.compile(r"([\\`*_[\]<>|])")
_FAILED_SCENARIO_LIMIT = 10


def fmt_s(ns):
    return f"{(ns or 0) / NS_PER_SEC:.1f}s"


def fmt_duration(ns):
    seconds = (ns or 0) / NS_PER_SEC
    if seconds < 60:
        return f"{seconds:.1f}s"

    total_seconds = round(seconds)
    hours, remainder = divmod(total_seconds, 3600)
    minutes, seconds = divmod(remainder, 60)
    if hours:
        return f"{hours}h {minutes}m {seconds}s"
    return f"{minutes}m {seconds}s"


def cell(s):
    return str(s).replace("|", "\\|")


def markdown(s):
    return _MARKDOWN_SPECIAL.sub(r"\\\1", str(s))


def scenario_status(steps):
    if not steps:
        return "skipped"
    return max(
        (s.get("result", {}).get("status", "") for s in steps),
        key=lambda st: _STATUS_PRIORITY.get(st, 0),
    )


def scenarios(features):
    for feature in features:
        for scenario in feature.get("elements", []):
            steps = scenario.get("steps", [])
            yield {
                "feature": feature.get("name", ""),
                "name": scenario.get("name", ""),
                "steps": steps,
                "status": scenario_status(steps),
                "duration": sum(
                    step.get("result", {}).get("duration", 0) for step in steps
                ),
            }


def render_summary(features, out):
    out.write("### Acceptance\n")
    for scenario in scenarios(features):
        out.write("\n")
        out.write(
            f"#### {cell(scenario['feature'])} / {cell(scenario['name'])} / "
            f"{scenario['status']} / {fmt_s(scenario['duration'])}\n"
        )
        if not scenario["steps"]:
            continue
        out.write("\n| Step | Status | Time |\n")
        out.write("| --- | --- | --- |\n")
        for step in scenario["steps"]:
            keyword = step.get("keyword", "").strip()
            name = step.get("name", "")
            label = f"{keyword} {name}".strip()
            result = step.get("result", {})
            out.write(
                f"| {cell(label)} | {result.get('status', '')} | "
                f"{fmt_s(result.get('duration', 0))} |\n"
            )


def first_failing_step(steps):
    for step in steps:
        status = step.get("result", {}).get("status", "")
        if status not in ("", "passed", "skipped"):
            keyword = step.get("keyword", "").strip()
            name = step.get("name", "")
            return f"{keyword} {name}".strip()
    return "unknown"


def render_comment(features, out):
    all_scenarios = list(scenarios(features))
    passed = sum(scenario["status"] == "passed" for scenario in all_scenarios)
    skipped = sum(scenario["status"] == "skipped" for scenario in all_scenarios)
    failed_scenarios = [
        scenario
        for scenario in all_scenarios
        if scenario["status"] not in ("passed", "skipped")
    ]

    out.write(
        f"{passed} passed · {len(failed_scenarios)} failed · {skipped} skipped\n"
    )
    if not failed_scenarios:
        return

    out.write("\n<details>\n")
    out.write(f"<summary>Failed scenarios ({len(failed_scenarios)})</summary>\n\n")
    for scenario in failed_scenarios[:_FAILED_SCENARIO_LIMIT]:
        out.write(
            f"- **{markdown(scenario['feature'])} / {markdown(scenario['name'])}**\n"
        )
        out.write(
            "  - First failing step: "
            f"{markdown(first_failing_step(scenario['steps']))}\n"
        )
        out.write(f"  - Duration: {fmt_duration(scenario['duration'])}\n")

    remaining = len(failed_scenarios) - _FAILED_SCENARIO_LIMIT
    if remaining > 0:
        out.write(f"- {remaining} more failed scenarios; see the workflow run.\n")
    out.write("\n</details>\n")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("report")
    parser.add_argument(
        "--format",
        choices=("summary", "comment"),
        default="summary",
    )
    args = parser.parse_args()

    with open(args.report) as f:
        features = json.load(f)
    if args.format == "comment":
        render_comment(features, sys.stdout)
    else:
        render_summary(features, sys.stdout)


if __name__ == "__main__":
    main()
