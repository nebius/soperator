#!/usr/bin/env python3
import io
import unittest

from acceptance_summary import NS_PER_SEC, render_comment
from e2e_pr_comment import render_final, render_start


def cucumber_step(name, status, seconds=1):
    return {
        "keyword": "Given ",
        "name": name,
        "result": {"status": status, "duration": seconds * NS_PER_SEC},
    }


def cucumber_scenario(feature, name, steps):
    return {
        "name": feature,
        "elements": [{"name": name, "steps": steps}],
    }


def context(**overrides):
    values = {
        "GITHUB_SERVER_URL": "https://github.com",
        "GITHUB_REPOSITORY": "nebius/soperator",
        "GITHUB_RUN_ID": "12345",
        "GITHUB_RUN_ATTEMPT": "1",
        "SOPERATOR_BRANCH": "feature",
        "TERRAFORM_BRANCH": "main",
        "SOPERATOR_E2E_BRANCH": "feature",
        "REQUESTED_PROFILE": "@auto-select",
        "RESOLVED_PROFILE": "KCS_B200",
        "RUN_UNSTABLE_TESTS": "false",
        "RUN_ESSENTIAL_TESTS": "true",
        "RESOLVE_RESULT": "success",
        "E2E_RESULT": "success",
        "ACCEPTANCE_OUTCOME": "success",
        "ACCEPTANCE_REPORT_AVAILABLE": "true",
    }
    values.update(overrides)
    return values


def workflow_jobs(before=None, acceptance="success", after=None):
    steps = [
        {"name": "Set up job", "number": 1, "conclusion": "success"},
        {"name": "Terraform Apply", "number": 2, "conclusion": before or "success"},
        {"name": "Acceptance Tests", "number": 3, "conclusion": acceptance},
        {"name": "Upload Acceptance Reports", "number": 4, "conclusion": "success"},
        {"name": "Terraform Destroy", "number": 5, "conclusion": after or "success"},
    ]
    return {
        "jobs": [
            {
                "name": "resolve-profile",
                "steps": [
                    {"name": "Resolve profile", "number": 1, "conclusion": "success"}
                ],
            },
            {"name": "e2e-test (KCS_B200)", "steps": steps},
        ]
    }


class AcceptanceCommentTest(unittest.TestCase):
    def test_renders_compact_failure_details(self):
        features = [
            cucumber_scenario(
                "Enroot *GPU*",
                "containers [see] GPUs",
                [
                    cucumber_step("the cluster exists", "passed", 2),
                    cucumber_step("the GPU smoke job succeeds", "failed", 65),
                ],
            ),
            cucumber_scenario(
                "Accounting",
                "jobs are recorded",
                [cucumber_step("sacct contains the job", "passed", 3)],
            ),
            cucumber_scenario("Skipped", "not selected", []),
        ]
        output = io.StringIO()

        render_comment(features, output)

        result = output.getvalue()
        self.assertIn("1 passed · 1 failed · 1 skipped", result)
        self.assertIn("Enroot \\*GPU\\* / containers \\[see\\] GPUs", result)
        self.assertIn("First failing step: Given the GPU smoke job succeeds", result)
        self.assertIn("Duration: 1m 7s", result)

    def test_limits_failed_scenarios(self):
        features = [
            cucumber_scenario(
                "Feature",
                f"Scenario {index}",
                [cucumber_step("failure", "failed")],
            )
            for index in range(12)
        ]
        output = io.StringIO()

        render_comment(features, output)

        result = output.getvalue()
        self.assertEqual(result.count("First failing step:"), 10)
        self.assertIn("2 more failed scenarios; see the workflow run.", result)


class E2EPRCommentTest(unittest.TestCase):
    def test_initial_comment_has_exact_run_and_parameters(self):
        result = render_start(context())

        self.assertIn("### ⏳ E2E running", result)
        self.assertIn("- Soperator branch: `feature`", result)
        self.assertIn("- Essential tests only: `true`", result)
        self.assertIn(
            "[Workflow run](https://github.com/nebius/soperator/actions/runs/12345)",
            result,
        )
        self.assertNotIn("Build All", result)

    def test_failure_before_acceptance_shows_only_first_failure(self):
        jobs = workflow_jobs(before="failure", after="failure")
        result = render_final(
            context(E2E_RESULT="failure", ACCEPTANCE_OUTCOME="skipped"),
            jobs,
            "",
        )

        self.assertIn("### Failure before acceptance", result)
        self.assertIn("Failed step: `Terraform Apply`", result)
        self.assertIn("Acceptance tests: not run", result)
        self.assertNotIn("Failure after acceptance", result)
        self.assertNotIn("Terraform Destroy", result)

    def test_acceptance_and_later_failure_are_both_reported(self):
        result = render_final(
            context(E2E_RESULT="failure", ACCEPTANCE_OUTCOME="failure"),
            workflow_jobs(acceptance="failure", after="failure"),
            "16 passed · 2 failed · 1 skipped",
        )

        self.assertIn("### Acceptance results", result)
        self.assertIn("16 passed · 2 failed · 1 skipped", result)
        self.assertIn("### Failure after acceptance", result)
        self.assertIn("Failed step: `Terraform Destroy`", result)

    def test_after_acceptance_failure_omits_successful_steps(self):
        result = render_final(
            context(E2E_RESULT="failure"),
            workflow_jobs(after="failure"),
            "18 passed · 0 failed · 0 skipped",
        )

        self.assertIn("18 passed · 0 failed · 0 skipped", result)
        self.assertIn("Failed step: `Terraform Destroy`", result)
        self.assertNotIn("Terraform Apply", result)

    def test_only_first_failure_after_acceptance_is_shown(self):
        jobs = workflow_jobs(after="failure")
        jobs["jobs"][1]["steps"][3]["conclusion"] = "failure"

        result = render_final(
            context(E2E_RESULT="failure"),
            jobs,
            "18 passed · 0 failed · 0 skipped",
        )

        self.assertIn("Failed step: `Upload Acceptance Reports`", result)
        self.assertNotIn("Terraform Destroy", result)

    def test_success_does_not_list_cleanup(self):
        result = render_final(
            context(),
            workflow_jobs(),
            "18 passed · 0 failed · 0 skipped",
        )

        self.assertIn("### ✅ E2E passed", result)
        self.assertNotIn("Terraform Destroy", result)

    def test_missing_acceptance_report_is_explicit(self):
        result = render_final(
            context(ACCEPTANCE_REPORT_AVAILABLE="false"),
            workflow_jobs(),
            "",
        )

        self.assertIn("Acceptance report unavailable", result)

    def test_rerun_links_to_attempt(self):
        result = render_start(context(GITHUB_RUN_ATTEMPT="2"))

        self.assertIn("/actions/runs/12345/attempts/2", result)


if __name__ == "__main__":
    unittest.main()
