import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import textwrap
from types import SimpleNamespace
import unittest


WORKFLOWS = Path(__file__).resolve().parents[1] / "workflows"


def shell_step(workflow, name):
    text = (WORKFLOWS / workflow).read_text()
    step = re.search(r"(?ms)^      - name: " + re.escape(name) + r"\n(.*?)(?=^      - name:|^  [a-z]|\Z)", text)[1]
    return textwrap.dedent(step.split("        run: |\n", 1)[1])


def expression(text, **contexts):
    text = text.removeprefix("${{").removesuffix("}}").strip()
    return eval(text.replace("&&", " and ").replace("||", " or "),
                {"__builtins__": {}, "always": lambda: True,
                 "startsWith": lambda value, prefix: value.startswith(prefix)}, contexts)


class PRCommandTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        directory = Path(self.tmp.name)
        self.output = directory / "output"
        self.dispatch = directory / "dispatch.json"
        self.env = dict(os.environ, PATH=f"{directory}:{os.environ['PATH']}",
                        GITHUB_OUTPUT=str(self.output), DISPATCH_FILE=str(self.dispatch),
                        REPOSITORY="nebius/soperator", PR_NUMBER="123", TERRAFORM_REPOSITORY="nebius/nebius-solution-library")
        scripts = {
            "gh": '''#!/usr/bin/env python3
import json,os,sys
args=sys.argv[1:]
if args[:2]==["workflow","run"]:
    with open(os.environ["DISPATCH_FILE"],"w") as out: json.dump(args,out)
elif args and args[0]=="api":
    print(json.dumps({"state":"open","head":{"ref":"feature","sha":"abc123","repo":{"full_name":"nebius/soperator"}}}))
''',
            "git": '''#!/bin/bash
if [[ "$1" == "ls-remote" ]]; then exit 0; fi
exec /usr/bin/git "$@"
''',
        }
        for name, content in scripts.items():
            path = directory / name
            path.write_text(content)
            path.chmod(0o755)

    def request(self, comment):
        self.output.write_text("")
        result = subprocess.run(["bash", "-eo", "pipefail", "-c", shell_step("e2e_pr_command.yml", "Validate request")],
                                env=dict(self.env, COMMENT_BODY=comment), capture_output=True, text=True)
        outputs = dict(line.split("=", 1) for line in self.output.read_text().splitlines())
        return result, outputs

    def test_commands_dispatch_expected_selection_and_pinned_sha(self):
        for command, heavy in (("/e2e", "false"), ("/e2e-full", "true")):
            with self.subTest(command=command):
                result, outputs = self.request(command)
                self.assertEqual(0, result.returncode, result.stdout + result.stderr)
                env = dict(self.env, HEAD_REF=outputs["head_ref"], HEAD_SHA=outputs["head_sha"],
                           TERRAFORM_REF=outputs["terraform_ref"], RUN_HEAVY=outputs["run_heavy"], SCENARIOS=outputs["scenarios"])
                dispatched = subprocess.run(["bash", "-eo", "pipefail", "-c", shell_step("e2e_pr_command.yml", "Dispatch E2E")],
                                            env=env, capture_output=True, text=True)
                self.assertEqual(0, dispatched.returncode, dispatched.stderr)
                args = json.loads(self.dispatch.read_text())
                self.assertIn(f"run_heavy={heavy}", args)
                self.assertIn("run_unstable_tests=false", args)
                self.assertIn("soperator_build_sha=abc123", args)
                self.assertIn("is_scheduled=false", args)

    def test_both_commands_accept_branch_and_repository_relative_selectors(self):
        selectors = "e2e/acceptance/features/active_checks.feature:3, features/observability.feature"
        for command in ("/e2e", "/e2e-full"):
            result, outputs = self.request(f'{command} terraform-branch --scenarios="{selectors}"')
            self.assertEqual(0, result.returncode, result.stdout + result.stderr)
            self.assertEqual("terraform-branch", outputs["terraform_ref"])
            self.assertEqual(selectors, outputs["scenarios"])

    def test_unsupported_commands_and_flags_are_rejected(self):
        for comment in ("/e2e-essential", "/e2e-full --run-unstable", "/e2e --run-heavy", "/e2e-extra"):
            with self.subTest(comment=comment):
                result, _ = self.request(comment)
                self.assertNotEqual(0, result.returncode)

    def test_acceptance_step_forwards_each_selector_and_tag_control(self):
        selectors = "e2e/acceptance/features/active_checks.feature:3; features/observability.feature,features/internal_ssh.feature:3"
        script = shell_step("e2e_test.yml", "Acceptance Tests")
        self.assertIn('--run-heavy', script)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "bin").mkdir()
            (root / "acceptance").mkdir()
            binary = root / "bin/acceptance"
            binary.write_text('#!/usr/bin/env python3\nimport json,os,sys\nwith open(os.environ["DISPATCH_FILE"],"w") as out: json.dump(sys.argv[1:],out)\n')
            binary.chmod(0o755)
            env = dict(self.env, SCENARIOS=selectors, RUN_HEAVY="true", RUN_UNSTABLE_TESTS="false",
                       SLURM_CLUSTER_NAME="soperator", SOPERATOR_VERSION="5.0.0", E2E_ARTIFACTS_DIR=directory)
            result = subprocess.run(["bash", "-eo", "pipefail", "-c", script], cwd=root, env=env, capture_output=True, text=True)
            self.assertEqual(0, result.returncode, result.stderr)
            args = json.loads(self.dispatch.read_text())
            self.assertIn("--run-heavy", args)
            self.assertNotIn("--run-unstable", args)
            selected = [args[i + 1] for i, arg in enumerate(args) if arg == "--scenario"]
            self.assertEqual(["e2e/acceptance/features/active_checks.feature:3", "features/observability.feature", "features/internal_ssh.feature:3"], selected)


class WorkflowSelectionTest(unittest.TestCase):
    def test_scheduled_runs_force_heavy_and_exclude_unstable(self):
        text = (WORKFLOWS / "e2e_test.yml").read_text()
        heavy = re.search(r"^  RUN_HEAVY: (.+)$", text, re.MULTILINE)[1]
        unstable = re.search(r"^  RUN_UNSTABLE_TESTS: (.+)$", text, re.MULTILINE)[1]
        for scheduled, requested_heavy, requested_unstable, want_heavy, want_unstable in (
                ("true", "false", "true", "true", "false"),
                ("false", "false", "false", "false", "false"),
                ("false", "true", "false", "true", "false"),
                ("false", "true", "true", "true", "true")):
            inputs = SimpleNamespace(is_scheduled=scheduled, run_heavy=requested_heavy, run_unstable_tests=requested_unstable)
            self.assertEqual(want_heavy, expression(heavy, inputs=inputs))
            self.assertEqual(want_unstable, expression(unstable, inputs=inputs))

    def test_only_periodic_default_ref_failures_notify_slack(self):
        text = (WORKFLOWS / "e2e_test.yml").read_text().split("  notify-failure:\n", 1)[1]
        condition = re.search(r"(?s)    if: \|\n(.*?)    runs-on:", text)[1]
        for scheduled, branch, result, custom_ref, want in (
                ("true", "main", "failure", "", True),
                ("true", "soperator-release-4.1", "failure", "", True),
                ("false", "main", "failure", "", False),
                ("false", "feature", "failure", "", False),
                ("true", "main", "success", "", False),
                ("true", "main", "failure", "custom", False)):
            inputs = SimpleNamespace(is_scheduled=scheduled, soperator_build_branch="", terraform_repo_ref=custom_ref, soperator_e2e_repo_branch="main")
            needs = SimpleNamespace(**{job: SimpleNamespace(result=result if job == "e2e-test" else "success")
                                      for job in ("resolve-build", "resolve-profile", "e2e-test")})
            # GitHub allows hyphens in context property names; Python uses getattr.
            python_condition = re.sub(r"needs\.([a-z0-9-]+)", r'getattr(needs, "\1")', condition)
            python_condition = " ".join(python_condition.split())
            python_condition = python_condition.replace("&&", " and ").replace("||", " or ")
            actual = eval(python_condition, {"__builtins__": {}, "always": lambda: True,
                          "startsWith": lambda value, prefix: value.startswith(prefix), "getattr": getattr},
                          dict(inputs=inputs, needs=needs, github=SimpleNamespace(ref_name=branch)))
            self.assertEqual(want, actual)


if __name__ == "__main__":
    unittest.main()
