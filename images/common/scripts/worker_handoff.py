#!/usr/bin/env python3

import argparse
import json
import socket
import ssl
import sys
import time
from http.client import HTTPException
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


SERVICE_ACCOUNT_DIR = Path("/var/run/secrets/kubernetes.io/serviceaccount")
WORKER_OPERATION_ID_LABEL = "slurm.nebius.ai/worker-operation-id"
WORKER_OPERATION_PHASE_LABEL = "slurm.nebius.ai/worker-operation-phase"
WORKER_OPERATION_PHASE_STOPPING = "stopping"
WORKER_OPERATION_PHASE_READY = "ready"
WORKER_OPERATION_ID_JSON_POINTER = "slurm.nebius.ai~1worker-operation-id"
WORKER_OPERATION_PHASE_JSON_POINTER = "slurm.nebius.ai~1worker-operation-phase"
MAX_ATTEMPTS = 5
REQUEST_TIMEOUT_SECONDS = 10


class KubernetesAPI:
    def __init__(self, pod_name, namespace, token, ca_cert):
        self.pod_url = (
            "https://kubernetes.default.svc/api/v1/namespaces/"
            f"{quote(namespace, safe='')}/pods/{quote(pod_name, safe='')}"
        )
        self.token = token
        self.ssl_context = ssl.create_default_context(cafile=str(ca_cert))

    def get_worker_operation(self):
        response = self._request("GET")
        try:
            pod = json.loads(response)
            metadata = pod["metadata"]
            labels = metadata.get("labels") or {}
            operation_id = labels.get(WORKER_OPERATION_ID_LABEL, "")
            phase = labels.get(WORKER_OPERATION_PHASE_LABEL, "")
        except (AttributeError, KeyError, TypeError) as error:
            raise ValueError("invalid Pod response from Kubernetes API") from error

        if not isinstance(operation_id, str) or not isinstance(phase, str):
            raise ValueError("invalid worker operation labels in Kubernetes API response")
        return operation_id, phase

    def mark_ready(self, operation_id):
        operation_id_path = (
            f"/metadata/labels/{WORKER_OPERATION_ID_JSON_POINTER}"
        )
        phase_path = f"/metadata/labels/{WORKER_OPERATION_PHASE_JSON_POINTER}"
        patch = [
            {
                "op": "test",
                "path": operation_id_path,
                "value": operation_id,
            },
            {
                "op": "test",
                "path": phase_path,
                "value": WORKER_OPERATION_PHASE_STOPPING,
            },
            {
                "op": "replace",
                "path": phase_path,
                "value": WORKER_OPERATION_PHASE_READY,
            },
        ]
        self._request(
            "PATCH",
            payload=patch,
            content_type="application/json-patch+json",
        )

    def _request(self, method, payload=None, content_type=None):
        headers = {
            "Accept": "application/json",
            "Authorization": f"Bearer {self.token}",
        }
        data = None
        if payload is not None:
            data = json.dumps(payload, separators=(",", ":")).encode()
        if content_type is not None:
            headers["Content-Type"] = content_type

        request = Request(self.pod_url, data=data, headers=headers, method=method)
        with urlopen(
            request,
            context=self.ssl_context,
            timeout=REQUEST_TIMEOUT_SECONDS,
        ) as response:
            return response.read()


def handoff_worker_operation(api, namespace, pod_name, sleep=time.sleep):
    operation_id = None

    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            current_operation_id, phase = api.get_worker_operation()
            if not current_operation_id:
                print(
                    f"Pod {namespace}/{pod_name} has no active worker operation",
                    file=sys.stderr,
                )
                return False

            if operation_id is None:
                operation_id = current_operation_id
            elif current_operation_id != operation_id:
                print(
                    f"Worker operation on Pod {namespace}/{pod_name} changed from "
                    f"{operation_id} to {current_operation_id}",
                    file=sys.stderr,
                )
                return False

            if phase == WORKER_OPERATION_PHASE_READY:
                return True
            if phase != WORKER_OPERATION_PHASE_STOPPING:
                print(
                    f"Worker operation {operation_id} on Pod {namespace}/{pod_name} "
                    f"is in unexpected phase {phase!r}",
                    file=sys.stderr,
                )
                return False

            api.mark_ready(operation_id)
            return True
        except (
            HTTPError,
            URLError,
            HTTPException,
            TimeoutError,
            OSError,
            ValueError,
        ) as error:
            if attempt == MAX_ATTEMPTS:
                print(
                    f"Failed to hand off worker operation on {namespace}/{pod_name} "
                    f"after {MAX_ATTEMPTS} attempts: {error}",
                    file=sys.stderr,
                )
                return False

            print(
                f"Attempt {attempt}/{MAX_ATTEMPTS} to hand off worker operation on "
                f"{namespace}/{pod_name} failed: {error}; retrying",
                file=sys.stderr,
            )
            sleep(attempt)

    return False


def read_service_account_file(name):
    value = (SERVICE_ACCOUNT_DIR / name).read_text().strip()
    if not value:
        raise ValueError(f"service account {name} is empty")
    return value


def parse_args(argv):
    parser = argparse.ArgumentParser(
        description="Hand off this Slurm worker pod to a Soperator worker operation."
    )
    return parser.parse_args(argv)


def main(argv=None):
    parse_args(argv)
    pod_name = socket.gethostname()

    try:
        namespace = read_service_account_file("namespace")
        token = read_service_account_file("token")
        api = KubernetesAPI(
            pod_name,
            namespace,
            token,
            SERVICE_ACCOUNT_DIR / "ca.crt",
        )
    except (OSError, ValueError) as error:
        print(f"Failed to initialize Kubernetes API client: {error}", file=sys.stderr)
        return 1

    return 0 if handoff_worker_operation(api, namespace, pod_name) else 1


if __name__ == "__main__":
    sys.exit(main())
