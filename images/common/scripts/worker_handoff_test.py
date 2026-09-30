#!/usr/bin/env python3

import io
import json
import unittest
from unittest import mock
from urllib.error import HTTPError, URLError

import worker_handoff


class HandoffWorkerOperationTest(unittest.TestCase):
    def setUp(self):
        self.api = mock.Mock(spec=worker_handoff.KubernetesAPI)
        self.sleep = mock.Mock()

    def handoff(self):
        return worker_handoff.handoff_worker_operation(
            self.api,
            "default",
            "worker-0",
            sleep=self.sleep,
        )

    def test_marks_stopping_operation_as_ready(self):
        self.api.get_worker_operation.return_value = ("revision-1", "stopping")

        self.assertTrue(self.handoff())

        self.api.mark_ready.assert_called_once_with("revision-1")
        self.sleep.assert_not_called()

    def test_already_ready_is_successful(self):
        self.api.get_worker_operation.return_value = ("revision-1", "ready")

        self.assertTrue(self.handoff())

        self.api.mark_ready.assert_not_called()
        self.sleep.assert_not_called()

    def test_missing_operation_id_fails_without_retry(self):
        self.api.get_worker_operation.return_value = ("", "stopping")

        with mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
            self.assertFalse(self.handoff())

        self.assertIn("has no active worker operation", stderr.getvalue())
        self.api.mark_ready.assert_not_called()
        self.sleep.assert_not_called()

    def test_unexpected_phase_fails_without_retry(self):
        self.api.get_worker_operation.return_value = ("revision-1", "")

        with mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
            self.assertFalse(self.handoff())

        self.assertIn("unexpected phase", stderr.getvalue())
        self.api.mark_ready.assert_not_called()
        self.sleep.assert_not_called()

    def test_patch_conflict_retries_and_accepts_ready_state(self):
        self.api.get_worker_operation.side_effect = [
            ("revision-1", "stopping"),
            ("revision-1", "ready"),
        ]
        self.api.mark_ready.side_effect = HTTPError(
            "https://kubernetes.default.svc",
            422,
            "JSON patch test failed",
            None,
            None,
        )

        with mock.patch("sys.stderr", new_callable=io.StringIO):
            self.assertTrue(self.handoff())

        self.assertEqual(2, self.api.get_worker_operation.call_count)
        self.api.mark_ready.assert_called_once_with("revision-1")
        self.sleep.assert_called_once_with(1)

    def test_changed_operation_id_is_not_acknowledged(self):
        self.api.get_worker_operation.side_effect = [
            ("revision-1", "stopping"),
            ("revision-2", "stopping"),
        ]
        self.api.mark_ready.side_effect = HTTPError(
            "https://kubernetes.default.svc",
            422,
            "JSON patch test failed",
            None,
            None,
        )

        with mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
            self.assertFalse(self.handoff())

        self.assertIn("changed from revision-1 to revision-2", stderr.getvalue())
        self.api.mark_ready.assert_called_once_with("revision-1")
        self.sleep.assert_called_once_with(1)

    def test_transient_errors_stop_after_max_attempts(self):
        self.api.get_worker_operation.side_effect = URLError("unavailable")

        with mock.patch("sys.stderr", new_callable=io.StringIO) as stderr:
            self.assertFalse(self.handoff())

        self.assertEqual(
            worker_handoff.MAX_ATTEMPTS,
            self.api.get_worker_operation.call_count,
        )
        self.assertEqual(
            [mock.call(1), mock.call(2), mock.call(3), mock.call(4)],
            self.sleep.call_args_list,
        )
        self.assertIn("after 5 attempts", stderr.getvalue())


class KubernetesAPITest(unittest.TestCase):
    def setUp(self):
        ssl_context = mock.Mock()
        with mock.patch.object(
            worker_handoff.ssl,
            "create_default_context",
            return_value=ssl_context,
        ):
            self.api = worker_handoff.KubernetesAPI(
                "worker-0",
                "default",
                "token",
                "/service-account/ca.crt",
            )

    def test_reads_worker_operation(self):
        pod = {
            "metadata": {
                "labels": {
                    worker_handoff.WORKER_OPERATION_ID_LABEL: "revision-1",
                    worker_handoff.WORKER_OPERATION_PHASE_LABEL: "stopping",
                }
            }
        }
        with mock.patch.object(
            self.api, "_request", return_value=json.dumps(pod).encode()
        ):
            self.assertEqual(
                ("revision-1", "stopping"),
                self.api.get_worker_operation(),
            )

    def test_mark_ready_uses_json_patch_with_operation_preconditions(self):
        with mock.patch.object(self.api, "_request") as request:
            self.api.mark_ready("revision-1")

        operation_id_path = (
            "/metadata/labels/slurm.nebius.ai~1worker-operation-id"
        )
        phase_path = "/metadata/labels/slurm.nebius.ai~1worker-operation-phase"
        request.assert_called_once_with(
            "PATCH",
            payload=[
                {
                    "op": "test",
                    "path": operation_id_path,
                    "value": "revision-1",
                },
                {"op": "test", "path": phase_path, "value": "stopping"},
                {"op": "replace", "path": phase_path, "value": "ready"},
            ],
            content_type="application/json-patch+json",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
