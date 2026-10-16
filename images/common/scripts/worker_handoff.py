#!/usr/bin/env python3

import argparse
import base64
import binascii
import json
import socket
import ssl
import sys
import time
from dataclasses import dataclass
from http.client import HTTPException
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen


SERVICE_ACCOUNT_DIR = Path("/var/run/secrets/kubernetes.io/serviceaccount")
WORKER_OPERATION_ID_LABEL = "slurm.nebius.ai/worker-operation-id"
WORKER_OPERATION_PHASE_LABEL = "slurm.nebius.ai/worker-operation-phase"
WORKER_OPERATION_PHASE_STOPPING = "stopping"
WORKER_OPERATION_PHASE_ACKNOWLEDGED = "acknowledged"
WORKER_OPERATION_ID_JSON_POINTER = "slurm.nebius.ai~1worker-operation-id"
WORKER_OPERATION_PHASE_JSON_POINTER = "slurm.nebius.ai~1worker-operation-phase"
MAX_ATTEMPTS = 5
REQUEST_TIMEOUT_SECONDS = 10


@dataclass(frozen=True)
class WorkerOperation:
    pod_uid: str
    operation_id: str
    phase: str


class KubernetesAPI:
    def __init__(self, pod_name, namespace, token, ca_cert, token_provider=None):
        self.pod_url = (
            "https://kubernetes.default.svc/api/v1/namespaces/"
            f"{quote(namespace, safe='')}/pods/{quote(pod_name, safe='')}"
        )
        self.token = token
        self.token_provider = token_provider
        self.ssl_context = ssl.create_default_context(cafile=str(ca_cert))

    def get_worker_operation(self):
        try:
            metadata = json.loads(self._request("GET"))["metadata"]
            labels = metadata.get("labels") or {}
            operation = WorkerOperation(
                metadata["uid"],
                labels.get(WORKER_OPERATION_ID_LABEL, ""),
                labels.get(WORKER_OPERATION_PHASE_LABEL, ""),
            )
        except (AttributeError, KeyError, TypeError) as error:
            raise ValueError("invalid Pod response from Kubernetes API") from error
        if (
            not isinstance(operation.pod_uid, str)
            or not operation.pod_uid
            or not isinstance(operation.operation_id, str)
            or not isinstance(operation.phase, str)
        ):
            raise ValueError("invalid worker operation identity or labels")
        return operation

    def mark_acknowledged(self, operation):
        operation_id_path = f"/metadata/labels/{WORKER_OPERATION_ID_JSON_POINTER}"
        phase_path = f"/metadata/labels/{WORKER_OPERATION_PHASE_JSON_POINTER}"
        self._request(
            "PATCH",
            payload=[
                {"op": "test", "path": "/metadata/uid", "value": operation.pod_uid},
                {"op": "test", "path": operation_id_path, "value": operation.operation_id},
                {"op": "test", "path": phase_path, "value": WORKER_OPERATION_PHASE_STOPPING},
                {"op": "replace", "path": phase_path, "value": WORKER_OPERATION_PHASE_ACKNOWLEDGED},
            ],
            content_type="application/json-patch+json",
        )

    def _request(self, method, payload=None, content_type=None):
        token = self.token_provider() if self.token_provider else self.token
        headers = {"Accept": "application/json", "Authorization": f"Bearer {token}"}
        data = None
        if payload is not None:
            data = json.dumps(payload, separators=(",", ":")).encode()
        if content_type is not None:
            headers["Content-Type"] = content_type
        request = Request(self.pod_url, data=data, headers=headers, method=method)
        with urlopen(request, context=self.ssl_context, timeout=REQUEST_TIMEOUT_SECONDS) as response:
            return response.read()


def handoff_worker_operation(api, namespace, pod_name, pod_uid, sleep=time.sleep):
    operation_id = None
    for attempt in range(1, MAX_ATTEMPTS + 1):
        try:
            operation = api.get_worker_operation()
            if operation.pod_uid != pod_uid or not operation.operation_id:
                print(f"Pod {namespace}/{pod_name} has no matching worker operation", file=sys.stderr)
                return False
            if operation_id is None:
                operation_id = operation.operation_id
            elif operation.operation_id != operation_id:
                print(f"Worker operation changed on Pod {namespace}/{pod_name}", file=sys.stderr)
                return False
            if operation.phase in (WORKER_OPERATION_PHASE_ACKNOWLEDGED, "recovering", "ready"):
                return True
            if operation.phase != WORKER_OPERATION_PHASE_STOPPING:
                print(
                    f"Worker operation on Pod {namespace}/{pod_name} has unexpected phase {operation.phase!r}",
                    file=sys.stderr,
                )
                return False
            api.mark_acknowledged(operation)
            return True
        except (HTTPError, URLError, HTTPException, TimeoutError, OSError, ValueError) as error:
            if attempt == MAX_ATTEMPTS:
                print(
                    f"Failed to acknowledge worker operation on {namespace}/{pod_name} "
                    f"after {MAX_ATTEMPTS} attempts: {error}",
                    file=sys.stderr,
                )
                return False
            print(
                f"Attempt {attempt}/{MAX_ATTEMPTS} to acknowledge worker operation on "
                f"{namespace}/{pod_name} failed: {error}; retrying",
                file=sys.stderr,
            )
            sleep(attempt)
    return False


def service_account_pod_uid(token, pod_name, namespace):
    try:
        parts = token.split(".")
        if len(parts) != 3:
            raise ValueError("invalid token format")
        payload = parts[1] + "=" * (-len(parts[1]) % 4)
        claims = json.loads(base64.b64decode(payload, altchars=b"-_", validate=True))
        kubernetes = claims["kubernetes.io"]
        pod = kubernetes["pod"]
        uid = pod["uid"]
        if not isinstance(uid, str) or not uid or pod["name"] != pod_name or kubernetes["namespace"] != namespace:
            raise ValueError("mismatched Pod identity")
        # Kubernetes authenticates this mounted, Pod-bound credential on each request.
        return uid
    except (KeyError, TypeError, ValueError, binascii.Error) as error:
        raise ValueError("service account token has no matching bound Pod identity") from error


def read_service_account_file(name):
    value = (SERVICE_ACCOUNT_DIR / name).read_text().strip()
    if not value:
        raise ValueError(f"service account {name} is empty")
    return value


def read_bound_service_account_token(pod_uid, pod_name, namespace):
    token = read_service_account_file("token")
    if service_account_pod_uid(token, pod_name, namespace) != pod_uid:
        raise ValueError("service account token Pod identity changed")
    return token


def main(argv=None):
    argparse.ArgumentParser(description="Acknowledge this Slurm worker's handoff to Soperator.").parse_args(argv)
    pod_name = socket.gethostname()
    try:
        namespace = read_service_account_file("namespace")
        token = read_service_account_file("token")
        pod_uid = service_account_pod_uid(token, pod_name, namespace)
        api = KubernetesAPI(
            pod_name, namespace, token, SERVICE_ACCOUNT_DIR / "ca.crt",
            token_provider=lambda: read_bound_service_account_token(pod_uid, pod_name, namespace),
        )
    except (OSError, ValueError) as error:
        print(f"Failed to initialize Kubernetes API client: {error}", file=sys.stderr)
        return 1
    return 0 if handoff_worker_operation(api, namespace, pod_name, pod_uid) else 1


if __name__ == "__main__":
    sys.exit(main())
