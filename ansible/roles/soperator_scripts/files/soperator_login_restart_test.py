#!/usr/bin/env python3

import base64
import importlib.machinery
import importlib.util
import io
import json
import sys
import unittest
from pathlib import Path
from unittest import mock
from urllib.error import HTTPError


SCRIPT = Path(__file__).with_name("soperator-login-restart")
LOADER = importlib.machinery.SourceFileLoader("login_restart", str(SCRIPT))
SPEC = importlib.util.spec_from_loader(LOADER.name, LOADER)
restart = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = restart
LOADER.exec_module(restart)
IDENTITY = restart.PodIdentity("login-0", "cluster", "pod-uid-1")


def pod_document():
    return {"metadata": {
        "name": IDENTITY.name,
        "namespace": IDENTITY.namespace,
        "uid": IDENTITY.uid,
        "resourceVersion": "42",
        "labels": {restart.COMPONENT_LABEL: "login"},
        "annotations": {
            restart.ANNOUNCED_ANNOTATION: "2026-10-15T12:00:00Z",
            restart.SCHEDULED_ANNOTATION: "2026-12-04T12:00:00Z",
            "unrelated.example/annotation": "preserve me",
        },
    }}


def token_for(identity=IDENTITY):
    claims = {"kubernetes.io": {
        "namespace": identity.namespace,
        "pod": {"name": identity.name, "uid": identity.uid},
    }}
    payload = base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=")
    return "header." + payload + ".signature"


class KubernetesAPITest(unittest.TestCase):
    def setUp(self):
        with mock.patch.object(restart.ssl, "create_default_context"):
            self.api = restart.KubernetesAPI(IDENTITY, Path("/service-account"))

    def test_http_patch_is_scoped_to_current_pod_and_conditioned_on_snapshot(self):
        document = pod_document()
        with mock.patch.object(restart, "read_service_account_file", return_value="token"), mock.patch.object(restart, "urlopen") as urlopen:
            urlopen.return_value.__enter__.return_value.read.side_effect = [json.dumps(document).encode(), b"{}"]
            pod = self.api.get_pod()
            self.api.request_restart(pod)
        requests = [call.args[0] for call in urlopen.call_args_list]
        self.assertEqual(["GET", "PATCH"], [request.method for request in requests])
        self.assertTrue(all(request.full_url == "https://kubernetes.default.svc/api/v1/namespaces/cluster/pods/login-0" for request in requests))
        self.assertEqual("application/json-patch+json", requests[1].get_header("Content-type"))
        self.assertEqual("Bearer token", requests[1].get_header("Authorization"))
        self.assertEqual([
            {"op": "test", "path": "/metadata/uid", "value": IDENTITY.uid},
            {"op": "test", "path": "/metadata/resourceVersion", "value": "42"},
            {"op": "add", "path": "/metadata/annotations/slurm.nebius.ai~1login-update-requested", "value": IDENTITY.uid},
        ], json.loads(requests[1].data))
        self.assertNotIn(restart.REQUESTED_ANNOTATION, pod.annotations)

    def test_stale_request_uid_is_replaced_only_for_current_live_pod(self):
        document = pod_document()
        document["metadata"]["annotations"][restart.REQUESTED_ANNOTATION] = "previous-pod-uid"
        with mock.patch.object(self.api, "_request") as request:
            self.api.request_restart(restart.parse_login_pod(document, IDENTITY))
        self.assertEqual(IDENTITY.uid, request.call_args.kwargs["payload"][-1]["value"])

    def test_each_request_reads_fresh_credentials(self):
        with mock.patch.object(restart, "read_service_account_file", side_effect=["first", "rotated"]) as read_token, mock.patch.object(restart, "urlopen") as urlopen:
            self.api._request("GET")
            self.api._request("GET")
        self.assertEqual([mock.call(Path("/service-account"), "token")] * 2, read_token.call_args_list)
        self.assertEqual(["Bearer first", "Bearer rotated"], [call.args[0].get_header("Authorization") for call in urlopen.call_args_list])


class PodIdentityTest(unittest.TestCase):
    def test_live_pod_uid_and_component_must_match(self):
        for field, value in (("uid", "replacement"), ("labels", {restart.COMPONENT_LABEL: "worker"})):
            document = pod_document()
            document["metadata"][field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                restart.parse_login_pod(document, IDENTITY)

    def test_token_extracts_bound_pod_identity(self):
        self.assertEqual(IDENTITY, restart.service_account_identity(token_for()))


class MainTest(unittest.TestCase):
    def setUp(self):
        self.pod = restart.parse_login_pod(pod_document(), IDENTITY)
        self.api = mock.Mock(spec=restart.KubernetesAPI)
        self.api.get_pod.return_value = self.pod
        self.stdout = io.StringIO()
        self.stderr = io.StringIO()
        self.stdin = mock.Mock()
        self.stdin.isatty.return_value = True
        self.prompt = mock.Mock(return_value="yes")
        self.constructor = mock.Mock(return_value=self.api)
        patches = [
            mock.patch.object(restart.os, "geteuid", return_value=0),
            mock.patch.object(restart, "SERVICE_ACCOUNT_DIR", Path("/fake-service-account")),
            mock.patch.object(restart, "read_service_account_file", return_value=token_for()),
            mock.patch.object(restart, "KubernetesAPI", self.constructor),
            mock.patch.object(restart, "urlopen", side_effect=AssertionError("unexpected live HTTP request")),
            mock.patch.object(restart.sys, "stdin", self.stdin),
            mock.patch.object(restart.sys, "stdout", self.stdout),
            mock.patch.object(restart.sys, "stderr", self.stderr),
            mock.patch("builtins.input", self.prompt),
        ]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def test_interactive_confirmation_precedes_write(self):
        def confirm(prompt):
            self.assertEqual("Restart login host login-0? Type yes/no: ", prompt)
            output = self.stdout.getvalue()
            self.assertIn("Login pod: cluster/login-0", output)
            self.assertIn("Update announced: 2026-10-15T12:00:00Z", output)
            self.assertIn("Scheduled restart: 2026-12-04T12:00:00Z", output)
            self.assertIn(restart.WARNING, output)
            self.assertNotIn("Early restart requested:", output)
            self.assertNotIn("Restart requested.", output)
            self.api.request_restart.assert_not_called()
            return "  YeS  "

        def submit(pod):
            self.assertIs(pod, self.pod)
            self.prompt.assert_called_once()
            self.assertNotIn("Restart requested.", self.stdout.getvalue())

        self.prompt.side_effect = confirm
        self.api.request_restart.side_effect = submit
        self.assertEqual(0, restart.main([]))
        self.api.get_pod.assert_called_once()
        self.api.request_restart.assert_called_once_with(self.pod)
        self.assertIn("Restart requested.", self.stdout.getvalue())

    def test_no_other_answer_or_interruption_authorizes_a_restart(self):
        for answer in ("no", "", "y", EOFError(), KeyboardInterrupt()):
            with self.subTest(answer=answer):
                self.api.reset_mock()
                self.prompt.reset_mock()
                self.stdout.seek(0)
                self.stdout.truncate()
                self.prompt.side_effect = answer if isinstance(answer, BaseException) else lambda prompt: answer
                self.assertEqual(0, restart.main([]))
                self.prompt.assert_called_once()
                self.api.get_pod.assert_called_once()
                self.api.request_restart.assert_not_called()
                self.assertIn("Restart cancelled.", self.stdout.getvalue())
                self.assertNotIn("Restart requested.", self.stdout.getvalue())

    def test_status_is_read_only_without_warning_or_prompt(self):
        self.stdin.isatty.return_value = False
        for requested in (False, True):
            with self.subTest(requested=requested):
                document = pod_document()
                if requested:
                    document["metadata"]["annotations"][restart.REQUESTED_ANNOTATION] = IDENTITY.uid
                self.api.get_pod.return_value = restart.parse_login_pod(document, IDENTITY)
                self.api.reset_mock()
                self.stdout.seek(0)
                self.stdout.truncate()
                self.assertEqual(0, restart.main(["--status"]))
                self.assertIn("Login pod: cluster/login-0", self.stdout.getvalue())
                self.assertIn(f"Early restart requested: {'yes' if requested else 'no'}", self.stdout.getvalue())
                self.assertNotIn(restart.WARNING, self.stdout.getvalue())
                self.prompt.assert_not_called()
                self.api.get_pod.assert_called_once()
                self.api.request_restart.assert_not_called()

    def test_yes_flag_submits_noninteractively_without_stale_status_line(self):
        self.stdin.isatty.return_value = False

        def submit(pod):
            self.assertIn(restart.WARNING, self.stdout.getvalue())
            self.assertNotIn("Restart requested.", self.stdout.getvalue())

        self.api.request_restart.side_effect = submit
        self.assertEqual(0, restart.main(["--yes"]))
        self.prompt.assert_not_called()
        self.api.get_pod.assert_called_once()
        self.api.request_restart.assert_called_once_with(self.pod)
        self.assertNotIn("Early restart requested:", self.stdout.getvalue())
        self.assertIn("Restart requested.", self.stdout.getvalue())

    def test_default_requires_terminal_or_explicit_action(self):
        self.stdin.isatty.return_value = False
        self.assertEqual(1, restart.main([]))
        self.assertIn("--yes", self.stderr.getvalue())
        self.assertIn("--status", self.stderr.getvalue())
        self.prompt.assert_not_called()
        self.api.request_restart.assert_not_called()
        self.assertNotIn("Restart requested.", self.stdout.getvalue())

    def test_existing_request_needs_no_further_confirmation_or_write(self):
        document = pod_document()
        document["metadata"]["annotations"][restart.REQUESTED_ANNOTATION] = IDENTITY.uid
        self.api.get_pod.return_value = restart.parse_login_pod(document, IDENTITY)
        self.stdin.isatty.return_value = False
        for arguments in ([], ["--yes"]):
            with self.subTest(arguments=arguments):
                self.assertEqual(0, restart.main(arguments))
                self.assertIn("already requested", self.stdout.getvalue())
                self.prompt.assert_not_called()
                self.api.request_restart.assert_not_called()

    def test_action_checks_schedule_and_termination_before_prompting_or_submitting(self):
        for arguments in ([], ["--yes"]):
            for invalid in (restart.ANNOUNCED_ANNOTATION, restart.SCHEDULED_ANNOTATION, "terminating"):
                with self.subTest(arguments=arguments, invalid=invalid):
                    document = pod_document()
                    if invalid == "terminating":
                        document["metadata"]["deletionTimestamp"] = "2026-10-16T12:00:00Z"
                    else:
                        del document["metadata"]["annotations"][invalid]
                    self.api.get_pod.return_value = restart.parse_login_pod(document, IDENTITY)
                    self.assertEqual(1, restart.main(arguments))
                    self.prompt.assert_not_called()
                    self.api.request_restart.assert_not_called()
        self.assertNotIn("Restart requested.", self.stdout.getvalue())

    def test_conflict_requires_retry_without_refreshing_or_resubmitting(self):
        self.api.request_restart.side_effect = HTTPError("https://kubernetes.default.svc", 409, "Conflict", None, None)
        self.assertEqual(1, restart.main([]))
        self.prompt.assert_called_once()
        self.api.get_pod.assert_called_once()
        self.api.request_restart.assert_called_once_with(self.pod)
        self.assertIn("inspect --status and retry", self.stderr.getvalue())
        self.assertNotIn("Restart requested.", self.stdout.getvalue())

    def test_failed_submission_does_not_print_success(self):
        self.api.request_restart.side_effect = HTTPError("https://kubernetes.default.svc", 403, "Forbidden", None, None)
        self.assertEqual(1, restart.main(["--yes"]))
        self.assertIn("HTTP 403", self.stderr.getvalue())
        self.assertNotIn("Restart requested.", self.stdout.getvalue())


if __name__ == "__main__":
    unittest.main(verbosity=2)
