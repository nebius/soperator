#!/usr/bin/env python3
"""Render the PR status comment for a dispatched E2E workflow."""
import argparse
import json
import os


UNSUCCESSFUL_CONCLUSIONS = {
    "action_required",
    "cancelled",
    "failure",
    "stale",
    "timed_out",
}


def inline_code(value):
    value = str(value).replace("\n", " ")
    delimiter = "`"
    while delimiter in value:
        delimiter += "`"
    padding = " " if value.startswith("`") or value.endswith("`") else ""
    return f"{delimiter}{padding}{value}{padding}{delimiter}"


def run_url(context):
    url = (
        f"{context['GITHUB_SERVER_URL']}/{context['GITHUB_REPOSITORY']}"
        f"/actions/runs/{context['GITHUB_RUN_ID']}"
    )
    attempt = int(context.get("GITHUB_RUN_ATTEMPT", "1"))
    if attempt > 1:
        return f"{url}/attempts/{attempt}"
    return url


def marker(context):
    return f"<!-- soperator-e2e-run:{context['GITHUB_RUN_ID']} -->"


def parameters(context, include_resolved_profile=False):
    values = [
        ("Soperator branch", context["SOPERATOR_BRANCH"]),
        ("Terraform branch", context["TERRAFORM_BRANCH"]),
        ("Soperator E2E branch", context["SOPERATOR_E2E_BRANCH"]),
        ("Profile", context["REQUESTED_PROFILE"]),
    ]
    resolved_profile = context.get("RESOLVED_PROFILE", "")
    if include_resolved_profile and resolved_profile:
        values.append(("Resolved profile", resolved_profile))
    values.extend(
        [
            ("Unstable tests", context["RUN_UNSTABLE_TESTS"]),
            ("Essential tests only", context["RUN_ESSENTIAL_TESTS"]),
        ]
    )
    return "\n".join(f"- {name}: {inline_code(value)}" for name, value in values)


def render_start(context):
    return "\n".join(
        [
            marker(context),
            "### ⏳ E2E running",
            "",
            "E2E was dispatched with:",
            "",
            parameters(context),
            "",
            f"[Workflow run]({run_url(context)})",
            "",
        ]
    )


def find_job(jobs, name=None, prefix=None):
    for job in jobs.get("jobs", []):
        if name is not None and job.get("name") == name:
            return job
        if prefix is not None and job.get("name", "").startswith(prefix):
            return job
    return None


def first_unsuccessful_step(job, before=None, after=None):
    if not job:
        return None
    for step in sorted(job.get("steps", []), key=lambda item: item.get("number", 0)):
        number = step.get("number", 0)
        if before is not None and number >= before:
            continue
        if after is not None and number <= after:
            continue
        if step.get("conclusion") in UNSUCCESSFUL_CONCLUSIONS:
            return step.get("name")
    return None


def acceptance_step_number(job):
    if not job:
        return None
    for step in job.get("steps", []):
        if step.get("name") == "Acceptance Tests":
            return step.get("number")
    return None


def result_header(context):
    results = (context["RESOLVE_RESULT"], context["E2E_RESULT"])
    if results == ("success", "success"):
        return "### ✅ E2E passed"
    if "cancelled" in results:
        return "### ⏹️ E2E cancelled"
    return "### ❌ E2E failed"


def acceptance_result(context, summary):
    outcome = context.get("ACCEPTANCE_OUTCOME", "")
    report_available = context.get("ACCEPTANCE_REPORT_AVAILABLE") == "true"
    if outcome == "skipped" or not outcome:
        return "Acceptance tests: not run"
    if outcome == "cancelled":
        return "Acceptance tests: cancelled"
    if report_available and summary.strip():
        return summary.strip()
    return "Acceptance report unavailable"


def render_final(context, jobs, acceptance_summary):
    resolve_job = find_job(jobs, name="resolve-profile")
    e2e_job = find_job(jobs, prefix="e2e-test (")
    acceptance_number = acceptance_step_number(e2e_job)

    before_failure = None
    after_failure = None
    if context["RESOLVE_RESULT"] != "success":
        before_failure = first_unsuccessful_step(resolve_job) or "resolve-profile"
    elif acceptance_number is None:
        before_failure = first_unsuccessful_step(e2e_job) or "e2e-test"
    else:
        before_failure = first_unsuccessful_step(e2e_job, before=acceptance_number)
        after_failure = first_unsuccessful_step(e2e_job, after=acceptance_number)

    sections = [
        marker(context),
        result_header(context),
        "",
        "E2E was dispatched with:",
        "",
        parameters(context, include_resolved_profile=True),
    ]

    if before_failure:
        sections.extend(
            [
                "",
                "### Failure before acceptance",
                "",
                f"- Failed step: {inline_code(before_failure)}",
                "- Acceptance tests: not run",
            ]
        )
    else:
        sections.extend(
            [
                "",
                "### Acceptance results",
                "",
                acceptance_result(context, acceptance_summary),
            ]
        )
        if after_failure:
            sections.extend(
                [
                    "",
                    "### Failure after acceptance",
                    "",
                    f"- Failed step: {inline_code(after_failure)}",
                ]
            )

    sections.extend(["", f"[Workflow run]({run_url(context)})", ""])
    return "\n".join(sections)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("phase", choices=("start", "final"))
    parser.add_argument("--jobs")
    parser.add_argument("--acceptance-summary")
    args = parser.parse_args()

    if args.phase == "start":
        print(render_start(os.environ), end="")
        return

    with open(args.jobs) as jobs_file:
        jobs = json.load(jobs_file)
    summary = ""
    if args.acceptance_summary:
        with open(args.acceptance_summary) as summary_file:
            summary = summary_file.read()
    print(render_final(os.environ, jobs, summary), end="")


if __name__ == "__main__":
    main()
