import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock


CHECK_PATH = Path(__file__).with_name("gpu_health_check.py")


def load_module():
    # The script runs the health-checker at import time; stop it before the environment lookup.
    spec = importlib.util.spec_from_file_location("gpu_health_check_under_test", CHECK_PATH)
    module = importlib.util.module_from_spec(spec)
    with mock.patch.dict(os.environ, {}, clear=True), mock.patch("sys.exit", side_effect=SystemExit):
        try:
            spec.loader.exec_module(module)
        except SystemExit:
            pass
    return module


class InjectErrorArgsTest(unittest.TestCase):
    def setUp(self):
        self.module = load_module()
        self.tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmpdir.cleanup)
        self.marker = os.path.join(self.tmpdir.name, "health_checker_inject_error")

    def write_marker(self, content: str) -> None:
        with open(self.marker, "w", encoding="utf-8") as f:
            f.write(content)

    def test_missing_marker_adds_nothing(self):
        self.assertEqual([], self.module.inject_error_args(self.marker))

    def test_supported_resolution_is_passed_through(self):
        for resolution in ("Unknown", "Redeploy", "RedeployProfile"):
            with self.subTest(resolution=resolution):
                self.write_marker(resolution + "\n")
                self.assertEqual(["--inject-error", resolution], self.module.inject_error_args(self.marker))

    def test_unsupported_content_is_ignored(self):
        for content in ("", "redeployprofile", "Empty", "RedeployProfile; rm -rf /"):
            with self.subTest(content=content):
                self.write_marker(content)
                self.assertEqual([], self.module.inject_error_args(self.marker))

    def test_default_marker_path_is_node_local(self):
        self.assertTrue(self.module.INJECT_ERROR_MARKER.startswith("/opt/soperator-outputs/local/"))


if __name__ == "__main__":
    unittest.main()
