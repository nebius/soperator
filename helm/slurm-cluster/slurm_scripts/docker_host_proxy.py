#!/usr/bin/env python3

import argparse
import json
import os
import re
import shutil
import socket
import socketserver
import sys
import urllib.parse
from http import client
from http.server import BaseHTTPRequestHandler
from typing import Optional


HOP_BY_HOP_HEADERS = {
    # "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    # "upgrade",
}
CONTAINER_CREATE_PATH_RE = re.compile(r"^(?:/v[0-9.]+)?/containers/create$")


class UnixHTTPConnection(client.HTTPConnection):
    def __init__(self, unix_socket_path: str, timeout: int = 300):
        super().__init__("localhost", timeout=timeout)
        self.unix_socket_path = unix_socket_path

    def connect(self) -> None:
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(self.timeout)
        sock.connect(self.unix_socket_path)
        self.sock = sock


class ThreadingUnixStreamServer(socketserver.ThreadingMixIn, socketserver.UnixStreamServer):
    daemon_threads = True


class DockerProxyHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def _maybe_patch_container_create_request(self) -> Optional[bytes]:
        content_length = int(self.headers.get("Content-Length", "0") or "0")
        if content_length < 0:
            return
        request_body = self.rfile.read(content_length)
        if self.command.upper() != "POST":
            return request_body

        if not CONTAINER_CREATE_PATH_RE.match(urllib.parse.urlsplit(self.path).path):
            return request_body

        try:
            payload = json.loads(request_body)
        except json.JSONDecodeError:
            return request_body

        if not isinstance(payload, dict):
            return request_body

        host_config = payload.get("HostConfig")
        if host_config is None:
            host_config = {}
            payload["HostConfig"] = host_config

        if not isinstance(host_config, dict):
            return request_body

        cgroup_parent = self.headers.get("Cgroup-Parent")
        if cgroup_parent and host_config.get("CgroupParent") in (None, ""):
            host_config["CgroupParent"] = cgroup_parent

        return json.dumps(payload, separators=(",", ":")).encode("utf-8")

    def handle(self):
        try:
            self._connection = UnixHTTPConnection(self.server.target_socket)
            super().handle()
        finally:
            self._connection.close()

    def _handle(self) -> None:
        request_body = self._maybe_patch_container_create_request()

        print(self.headers)
        upstream_headers = {k: v for k, v in self.headers.items() if k.lower() not in HOP_BY_HOP_HEADERS}
        if request_body is not None:
            upstream_headers["Content-Length"] = str(len(request_body))

        self._connection.request(self.command, self.path, headers=upstream_headers, body=request_body if request_body is not None else self.rfile)
        response = self._connection.getresponse()
        self.send_response(response.status, response.reason)
        for key, value in response.getheaders():
            if key.lower() in HOP_BY_HOP_HEADERS:
                continue
            self.send_header(key, value)
        self.end_headers()
        print(response.status, response.getheaders())
        if response.status == 101:
            self.close_connection = True
            shutil.copyfileobj(response.fp, self.wfile)
        else:
            shutil.copyfileobj(response, self.wfile)
        self.wfile.flush()

    def do_GET(self) -> None:  # noqa: N802
        self._handle()

    def do_POST(self) -> None:  # noqa: N802
        self._handle()

    def do_PUT(self) -> None:  # noqa: N802
        self._handle()

    def do_DELETE(self) -> None:  # noqa: N802
        self._handle()

    def do_PATCH(self) -> None:  # noqa: N802
        self._handle()

    def do_HEAD(self) -> None:  # noqa: N802
        self._handle()

    def do_OPTIONS(self) -> None:  # noqa: N802
        self._handle()

    def log_message(self, fmt: str, *args: object) -> None:
        client = "-"
        if isinstance(self.client_address, tuple) and self.client_address:
            client = str(self.client_address[0])
        elif isinstance(self.client_address, str) and self.client_address:
            client = self.client_address
        message = "%s - - [%s] %s" % (client, self.log_date_time_string(), fmt % args)
        print(message, file=sys.stderr, flush=True)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Proxy Docker Engine API over a Unix socket")
    parser.add_argument("--listen-socket", required=True, help="Unix socket path to listen on")
    parser.add_argument("--target-socket", required=True, help="Unix socket path of real Docker daemon")
    return parser.parse_args()


def main() -> int:
    args = parse_args()

    listen_socket = args.listen_socket
    target_socket = args.target_socket

    os.makedirs(os.path.dirname(listen_socket), exist_ok=True)
    if os.path.exists(listen_socket):
        os.unlink(listen_socket)

    with ThreadingUnixStreamServer(listen_socket, DockerProxyHandler) as server:
        server.target_socket = target_socket  # type: ignore[attr-defined]
        os.chmod(listen_socket, 0o666)
        server.serve_forever()

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
