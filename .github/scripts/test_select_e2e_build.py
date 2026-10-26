import io
from contextlib import redirect_stdout
import unittest
import zipfile
from unittest.mock import Mock

from select_e2e_build import GitHub, InvalidBuildMetadata, select_build


VERSIONS = dict(soperator_version="5.0.0-rabc", soperator_unstable="true", nfs_version="1.0.0")


def build(run_id=10, sha="head", status="completed", conclusion="success"):
    return dict(id=run_id, head_sha=sha, head_branch="feature", status=status,
                conclusion=conclusion, created_at=f"2026-10-26T00:00:{run_id:02d}Z",
                html_url=f"https://github.com/nebius/soperator/actions/runs/{run_id}")


def artifact(**overrides):
    return dict(id=1, name="version", expired=False, expires_at="2099-01-01T00:00:00Z", **overrides)


class Clock:
    def __init__(self):
        self.now = 0

    def __call__(self):
        return self.now

    def sleep(self, seconds):
        self.now += seconds


class FakeGitHub:
    def __init__(self, runs, polls=(), artifacts=None):
        self.runs = runs
        self.polls = iter(polls)
        self.calls = []
        self.artifacts = artifacts if artifacts is not None else [artifact()]
        self.version = Mock(return_value=VERSIONS)

    def api(self, path, **params):
        self.calls.append((path, params))
        if path.endswith("one_job.yml/runs"):
            return {"workflow_runs": self.runs.get(params["head_sha"], [])}
        if path.endswith("/artifacts"):
            return {"artifacts": self.artifacts}
        return next(self.polls)


class BuildSelectionTest(unittest.TestCase):
    def select(self, github, scheduled=False, commits=None, **options):
        self.clock = Clock()
        with redirect_stdout(io.StringIO()):
            return select_build(github, "feature", commits or ["head"], scheduled,
                                clock=self.clock, sleep=self.clock.sleep, **options)

    def test_latest_success_is_selected(self):
        github = FakeGitHub({"head": [build(9), build(10)]})
        result = self.select(github)
        self.assertEqual("ready", result.status)
        self.assertEqual(10, result.run["id"])
        self.assertEqual(VERSIONS, result.versions)

    def test_latest_failure_never_falls_back(self):
        github = FakeGitHub({"head": [build(9), build(10, conclusion="failure")]})
        result = self.select(github)
        self.assertEqual("skipped", result.status)
        self.assertEqual(10, result.run["id"])
        self.assertIn("failure", result.reason)
        github.version.assert_not_called()

    def test_waits_for_pinned_build_despite_newer_builds(self):
        github = FakeGitHub(
            {"head": [build(status="queued", conclusion=None)]},
            [build(status="in_progress", conclusion=None), build()],
        )

        def new_build_appears(run):
            github.runs["head"].append(build(11))

        result = self.select(github, on_wait=new_build_appears)
        self.assertEqual("ready", result.status)
        self.assertEqual(10, result.run["id"])
        self.assertEqual(60, self.clock.now)
        self.assertEqual(1, sum(path.endswith("one_job.yml/runs") for path, _ in github.calls))
        polled = [path for path, params in github.calls if not params]
        self.assertEqual(["actions/runs/10", "actions/runs/10"], polled)

    def test_failure_after_wait_skips(self):
        github = FakeGitHub({"head": [build(status="in_progress", conclusion=None)]},
                            [build(conclusion="cancelled")])
        result = self.select(github)
        self.assertEqual("skipped", result.status)
        self.assertIn("cancelled", result.reason)

    def test_wait_is_bounded_to_one_hour(self):
        pending = build(status="in_progress", conclusion=None)
        github = FakeGitHub({"head": [pending]}, [pending] * 120)
        result = self.select(github)
        self.assertEqual("skipped", result.status)
        self.assertIn("one hour", result.reason)
        self.assertEqual(3600, self.clock.now)

    def test_missing_head_build_does_not_use_previous_commit(self):
        github = FakeGitHub({"older": [build(9, sha="older")]})
        result = self.select(github, commits=["head", "older"])
        self.assertEqual("skipped", result.status)
        self.assertIn("No build exists", result.reason)
        self.assertTrue(all(params["head_sha"] == "head" for _, params in github.calls))

    def test_runs_for_other_branches_are_not_used(self):
        wrong_branch = dict(build(), head_branch="other")
        result = self.select(FakeGitHub({"head": [wrong_branch]}))
        self.assertEqual("skipped", result.status)

    def test_expired_or_missing_artifact_skips(self):
        for artifacts in ([], [dict(artifact(), expired=True)],
                          [dict(artifact(), expires_at="2020-01-01T00:00:00Z")]):
            with self.subTest(artifacts=artifacts):
                result = self.select(FakeGitHub({"head": [build()]}, artifacts=artifacts))
                self.assertEqual("skipped", result.status)
                self.assertIn("version artifact", result.reason)

    def test_invalid_metadata_skips(self):
        github = FakeGitHub({"head": [build()]})
        github.version.side_effect = InvalidBuildMetadata("missing NFS version")
        result = self.select(github)
        self.assertEqual("skipped", result.status)
        self.assertIn("missing NFS version", result.reason)

    def test_periodic_selection_keeps_successful_fallback(self):
        github = FakeGitHub({"head": [build(conclusion="failure")],
                            "older": [build(9, sha="older")]})
        result = self.select(github, scheduled=True, commits=["head", "older"])
        self.assertEqual("ready", result.status)
        self.assertEqual(9, result.run["id"])

    def test_periodic_selection_skips_invalid_artifacts_and_running_builds(self):
        github = FakeGitHub({"head": [build(12, status="in_progress", conclusion=None), build(11), build(10)]})
        github.version.side_effect = [InvalidBuildMetadata("missing version"), VERSIONS]
        result = self.select(github, scheduled=True)
        self.assertEqual(10, result.run["id"])
        self.assertEqual(0, self.clock.now)

    def test_periodic_selection_fails_if_no_build_is_usable(self):
        with self.assertRaisesRegex(RuntimeError, "no valid successful build"):
            self.select(FakeGitHub({"head": [build(conclusion="failure")]}), scheduled=True)

    def test_api_errors_are_failures(self):
        github = FakeGitHub({})
        github.api = Mock(side_effect=RuntimeError("authentication failed"))
        with self.assertRaisesRegex(RuntimeError, "authentication failed"):
            self.select(github)


class VersionMetadataTest(unittest.TestCase):
    def test_version_archive_is_validated(self):
        for content, valid in ((b"5.0.0-rabc\ntrue\n1.0.0\n", True),
                               (b"5.0.0\ntrue\n", False), (b"bad\ntrue\n1.0.0\n", False),
                               (b"5.0.0\nmaybe\n1.0.0\n", False)):
            with self.subTest(content=content):
                data = io.BytesIO()
                with zipfile.ZipFile(data, "w") as archive:
                    archive.writestr("version.txt", content)
                github = GitHub("nebius/soperator")
                github.request = Mock(return_value=data.getvalue())
                if valid:
                    self.assertEqual(VERSIONS, github.version(artifact()))
                else:
                    with self.assertRaises(InvalidBuildMetadata):
                        github.version(artifact())


if __name__ == "__main__":
    unittest.main()
