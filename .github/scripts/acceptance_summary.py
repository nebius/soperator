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

_FAILED_SCENARIO_LIMIT = 10
_ERROR_MESSAGE_LIMIT = 4_000


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


def scenarios(features):
    for feature in features:
        for scenario in feature.get("elements", []):
            steps = scenario.get("steps", [])
            yield {
                "feature": feature.get("name", ""),
                "name": scenario.get("name", ""),
                "uri": feature.get("uri", ""),
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


def failed_step(steps):
    for step in steps:
        result = step.get("result", {})
        status = result.get("status", "")
        if status not in ("", "passed", "skipped"):
            return step
    return None


def truncate_error_message(message):
    message = str(message).strip()
    truncation_notice = "\n… (truncated; see workflow run)"
    if len(message) > _ERROR_MESSAGE_LIMIT:
        message = message[: _ERROR_MESSAGE_LIMIT - len(truncation_notice)]
        message += truncation_notice
    return message


def render_failed_steps(failed_scenarios, remaining, out):
    lines = ["--- Failed steps:"]
    for scenario in failed_scenarios:
        step = failed_step(scenario["steps"])
        scenario_location = scenario["uri"] or "unknown"
        lines.extend(
            [
                "",
                f"  Scenario: {scenario['name']} # {scenario_location}",
            ]
        )
        if step is None:
            lines.append("    unknown")
            continue

        keyword = step.get("keyword", "").strip()
        name = step.get("name", "")
        step_label = f"{keyword} {name}".strip()
        step_line = step.get("line")
        step_location = scenario_location
        if scenario_location != "unknown" and step_line is not None:
            step_location = f"{scenario_location}:{step_line}"
        lines.append(f"    {step_label} # {step_location}")

        error_message = step.get("result", {}).get("error_message", "")
        if error_message:
            error_lines = truncate_error_message(error_message).split("\n")
            lines.append(f"      Error: {error_lines[0]}")
            lines.extend(f"             {line}" for line in error_lines[1:])

    if remaining > 0:
        lines.extend(
            [
                "",
                f"  {remaining} more failed scenarios; see the workflow run.",
            ]
        )

    body = "\n".join(lines)

    backtick_runs = re.findall(r"`+", body)
    fence_length = max(3, max(map(len, backtick_runs), default=0) + 1)
    fence = "`" * fence_length
    out.write(f"{fence}text\n{body}\n{fence}\n")


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
    remaining = len(failed_scenarios) - _FAILED_SCENARIO_LIMIT
    render_failed_steps(
        failed_scenarios[:_FAILED_SCENARIO_LIMIT],
        remaining,
        out,
    )
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
