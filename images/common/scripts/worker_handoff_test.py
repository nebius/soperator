#!/usr/bin/env python3

import base64
import io
import json
import unittest
from dataclasses import replace
from unittest import mock
from urllib.error import HTTPError, URLError

import worker_handoff as handoff


POD_UID = "pod-uid-1"


def operation(phase="stopping", **kwargs):
    return replace(handoff.WorkerOperation(POD_UID, "revision-1", phase), **kwargs)


class HandoffWorkerOperationTest(unittest.TestCase):
    def setUp(self):
        self.api = mock.Mock(spec=handoff.KubernetesAPI)
        self.api.get_worker_operation.return_value = operation()
        self.sleep = mock.Mock()
        self.stderr = mock.patch.object(handoff.sys, "stderr", new_callable=io.StringIO)
        self.stderr.start()
        self.addCleanup(self.stderr.stop)

    def handoff(self):
        return handoff.handoff_worker_operation(self.api, "default", "worker-0", POD_UID, sleep=self.sleep)

    def test_acknowledges_and_exits_immediately(self):
        self.assertTrue(self.handoff())
        self.api.get_worker_operation.assert_called_once_with()
        self.api.mark_acknowledged.assert_called_once_with(operation())
        self.sleep.assert_not_called()

    def test_conflict_rechecks_identity_before_retrying(self):
        self.api.mark_acknowledged.side_effect = [HTTPError("https://kubernetes.default.svc", 422, "conflict", None, None), None]
        self.assertTrue(self.handoff())
        self.assertEqual(2, self.api.get_worker_operation.call_count)
        self.assertEqual([mock.call(operation())] * 2, self.api.mark_acknowledged.call_args_list)
        self.sleep.assert_called_once_with(1)

    def test_lost_patch_response_accepts_controller_progress(self):
        for phase in ("acknowledged", "recovering", "ready"):
            with self.subTest(phase=phase):
                self.api.reset_mock()
                self.api.get_worker_operation.side_effect = [operation(), operation(phase)]
                self.api.mark_acknowledged.side_effect = URLError("lost response")
                self.assertTrue(self.handoff())
                self.api.mark_acknowledged.assert_called_once_with(operation())

    def test_changed_pod_or_operation_after_conflict_is_not_acknowledged(self):
        for changed in (operation(pod_uid="replacement"), operation(operation_id="revision-2")):
            with self.subTest(changed=changed):
                self.api.reset_mock()
                self.api.get_worker_operation.side_effect = [operation(), changed]
                self.api.mark_acknowledged.side_effect = HTTPError("https://kubernetes.default.svc", 422, "conflict", None, None)
                self.assertFalse(self.handoff())
                self.api.mark_acknowledged.assert_called_once_with(operation())

    def test_replacement_pod_is_rejected_on_first_read(self):
        self.api.get_worker_operation.return_value = operation(pod_uid="replacement")
        self.assertFalse(self.handoff())
        self.api.mark_acknowledged.assert_not_called()
        self.sleep.assert_not_called()

    def test_missing_operation_and_unknown_phase_are_rejected(self):
        for op in (operation(operation_id=""), operation("unrecognized")):
            with self.subTest(op=op):
                self.api.get_worker_operation.return_value = op
                self.assertFalse(self.handoff())
        self.api.mark_acknowledged.assert_not_called()

    def test_transient_errors_stop_after_bounded_attempts(self):
        self.api.get_worker_operation.side_effect = URLError("unavailable")
        self.assertFalse(self.handoff())
        self.assertEqual(handoff.MAX_ATTEMPTS, self.api.get_worker_operation.call_count)
        self.assertEqual([mock.call(1), mock.call(2), mock.call(3), mock.call(4)], self.sleep.call_args_list)
        self.api.mark_acknowledged.assert_not_called()


class KubernetesAPITest(unittest.TestCase):
    def setUp(self):
        with mock.patch.object(handoff.ssl, "create_default_context"):
            self.api = handoff.KubernetesAPI("worker-0", "default", "token", "/service-account/ca.crt")

    def pod(self):
        return {"metadata": {"uid": POD_UID, "labels": {
            handoff.WORKER_OPERATION_ID_LABEL: "revision-1",
            handoff.WORKER_OPERATION_PHASE_LABEL: "stopping",
        }}}

    def test_acknowledges_parsed_pod_with_uid_operation_and_phase_preconditions(self):
        with mock.patch.object(self.api, "_request", return_value=json.dumps(self.pod())) as request:
            self.api.mark_acknowledged(self.api.get_worker_operation())
        request.assert_called_with(
            "PATCH", content_type="application/json-patch+json", payload=[
                {"op": "test", "path": "/metadata/uid", "value": POD_UID},
                {"op": "test", "path": "/metadata/labels/slurm.nebius.ai~1worker-operation-id", "value": "revision-1"},
                {"op": "test", "path": "/metadata/labels/slurm.nebius.ai~1worker-operation-phase", "value": "stopping"},
                {"op": "replace", "path": "/metadata/labels/slurm.nebius.ai~1worker-operation-phase", "value": "acknowledged"},
            ],
        )

    def test_api_rereads_projected_token_on_every_request(self):
        self.api.token_provider = mock.Mock(side_effect=["token-1", "token-2"])
        with mock.patch.object(handoff, "urlopen") as urlopen:
            self.api._request("GET")
            self.api._request("GET")
        self.assertEqual(
            ["Bearer token-1", "Bearer token-2"],
            [call.args[0].get_header("Authorization") for call in urlopen.call_args_list],
        )


class ServiceAccountPodIdentityTest(unittest.TestCase):
    def token(self, claims):
        payload = base64.urlsafe_b64encode(json.dumps(claims).encode()).decode().rstrip("=")
        return "header." + payload + ".signature"

    def claims(self):
        return {"kubernetes.io": {"namespace": "default", "pod": {"name": "worker-0", "uid": POD_UID}}}

    def test_rotated_token_must_match_original_pod_uid(self):
        token = self.token(self.claims())
        with mock.patch.object(handoff, "read_service_account_file", return_value=token):
            self.assertEqual(token, handoff.read_bound_service_account_token(POD_UID, "worker-0", "default"))
            with self.assertRaisesRegex(ValueError, "Pod identity changed"):
                handoff.read_bound_service_account_token("replacement", "worker-0", "default")

    def test_unbound_or_mismatched_tokens_fail_closed(self):
        invalid = ["legacy-token", "header.%%%.signature", self.token({})]
        for field, value in (("uid", ""), ("name", "worker-1")):
            claims = self.claims()
            claims["kubernetes.io"]["pod"][field] = value
            invalid.append(self.token(claims))
        claims = self.claims()
        claims["kubernetes.io"]["namespace"] = "other"
        invalid.append(self.token(claims))
        for token in invalid:
            with self.subTest(token=token):
                with self.assertRaisesRegex(ValueError, "no matching bound Pod identity"):
                    handoff.service_account_pod_uid(token, "worker-0", "default")


if __name__ == "__main__":
    unittest.main(verbosity=2)
