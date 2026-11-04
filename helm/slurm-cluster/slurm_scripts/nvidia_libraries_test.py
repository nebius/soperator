import importlib.util
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


CHECK_PATH = Path(__file__).with_name("nvidia_libraries.py")
PLACEHOLDER_MARKER = b"soperator gpu placeholder\n"
ELF = b"\x7fELF" + b"x" * 4096
ARCH_DIR = "/usr/lib/x86_64-linux-gnu"


def load_module():
    spec = importlib.util.spec_from_file_location("nvidia_libraries_under_test", CHECK_PATH)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def cli_result(returncode: int, stdout: str) -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess(args=["nvidia-container-cli"], returncode=returncode, stdout=stdout)


class NvidiaLibrariesTest(unittest.TestCase):
    def setUp(self):
        self.module = load_module()
        self.tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmpdir.cleanup)
        self.lib_dir = self.tmpdir.name
        self.module.LIB_DIR = self.lib_dir

        self.details = []
        self.enterContext(mock.patch.object(self.module, "write_details", side_effect=self.details.append))
        self.cdll = self.enterContext(mock.patch.object(self.module.ctypes, "CDLL", side_effect=self.fake_dlopen))
        self.run = self.enterContext(mock.patch.object(self.module.subprocess, "run"))
        self.enterContext(mock.patch.dict(os.environ, {"NODESET_GPU_ENABLED": "true"}))

        # A healthy jail: one real driver library and both sonames pointing at it
        self.real_lib = self.write_lib("libcuda.so.570.148.08", ELF)
        for soname in self.module.REQUIRED_SONAMES:
            os.symlink(self.real_lib, os.path.join(self.lib_dir, soname))
        self.run.return_value = cli_result(0, f"{ARCH_DIR}/{os.path.basename(self.real_lib)}\n")

    # Behaves like glibc dlopen on the files in lib_dir: sonames are looked up there as ld.so.cache would
    def fake_dlopen(self, name: str, mode=None):
        path = name if os.path.isabs(name) else os.path.join(self.lib_dir, name)
        try:
            size = os.stat(path).st_size
        except FileNotFoundError:
            raise OSError(f"{name}: cannot open shared object file: No such file or directory")
        if size < 64:
            raise OSError(f"{path}: file too short")
        with open(path, "rb") as f:
            if f.read(4) != b"\x7fELF":
                raise OSError(f"{path}: invalid ELF header")
        return mock.DEFAULT

    def write_lib(self, name: str, content: bytes) -> str:
        path = os.path.join(self.lib_dir, name)
        if os.path.lexists(path):
            os.remove(path)
        with open(path, "wb") as f:
            f.write(content)
        return path

    def test_skips_non_gpu_worker(self):
        with mock.patch.dict(os.environ, {"NODESET_GPU_ENABLED": "false"}):
            with self.assertLogs(self.module.LOGGER, level="INFO") as logs:
                self.assertEqual(0, self.module.main())
        self.assertIn("Not a GPU worker, skipping", "\n".join(logs.output))
        self.cdll.assert_not_called()
        self.run.assert_not_called()
        self.assertEqual([], self.details)

    def test_passes_when_everything_is_loadable(self):
        with self.assertLogs(self.module.LOGGER, level="INFO") as logs:
            self.assertEqual(0, self.module.main())
        output = "\n".join(logs.output)
        self.assertIn("Loaded libcuda.so.1", output)
        self.assertIn("Loaded libnvidia-ml.so.1", output)
        self.assertIn("listed 1 libraries, 1 belong to the compute and utility capabilities", output)
        self.assertIn("All 3 NVIDIA libraries are loadable", output)
        mode = self.module.ctypes.RTLD_LOCAL | os.RTLD_LAZY
        self.assertEqual(
            [
                mock.call("libcuda.so.1", mode=mode),
                mock.call("libnvidia-ml.so.1", mode=mode),
                mock.call(self.real_lib, mode=mode),
            ],
            self.cdll.call_args_list,
        )
        self.assertEqual([], self.details)

    def test_fails_when_libcuda_soname_points_to_empty_file(self):
        self.write_lib("libcuda.so.1", b"")
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libcuda.so.1: file too short"], self.details)

    def test_fails_when_libcuda_soname_is_not_loadable(self):
        self.write_lib("libcuda.so.1", b"not an elf" * 500)
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libcuda.so.1: not loadable"], self.details)

    def test_fails_when_nvml_soname_is_missing(self):
        os.remove(os.path.join(self.lib_dir, "libnvidia-ml.so.1"))
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libnvidia-ml.so.1: missing"], self.details)

    def test_fails_when_soname_is_dangling_symlink(self):
        os.remove(os.path.join(self.lib_dir, "libcuda.so.1"))
        os.symlink(os.path.join(self.lib_dir, "libcuda.so.575.57.08"), os.path.join(self.lib_dir, "libcuda.so.1"))
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libcuda.so.1: missing"], self.details)

    def test_skips_file_check_when_cli_fails(self):
        self.run.return_value = cli_result(1, "nvidia-container-cli: initialization error\n")
        with self.assertLogs(self.module.LOGGER, level="WARNING") as logs:
            self.assertEqual(0, self.module.main())
        self.assertIn("skipping the driver library file check", "\n".join(logs.output))
        self.assertIn("initialization error", "\n".join(logs.output))
        self.assertEqual([], self.details)

    def test_skips_file_check_when_cli_is_missing(self):
        self.run.side_effect = FileNotFoundError("nvidia-container-cli")
        with self.assertLogs(self.module.LOGGER, level="WARNING") as logs:
            self.assertEqual(0, self.module.main())
        self.assertIn("skipping the driver library file check", "\n".join(logs.output))
        self.assertEqual([], self.details)

    def test_fails_on_empty_library_file(self):
        self.write_lib("libnvidia-ml.so.570.148.08", b"")
        self.run.return_value = cli_result(
            0, f"{ARCH_DIR}/libcuda.so.570.148.08\n{ARCH_DIR}/libnvidia-ml.so.570.148.08\n"
        )
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libnvidia-ml.so.570.148.08: file too short"], self.details)

    def test_fails_on_placeholder_library_file(self):
        self.write_lib("libnvidia-ptxjitcompiler.so.570.148.08", PLACEHOLDER_MARKER)
        self.run.return_value = cli_result(0, f"{ARCH_DIR}/libnvidia-ptxjitcompiler.so.570.148.08\n")
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libnvidia-ptxjitcompiler.so.570.148.08: file too short"], self.details)

    def test_fails_on_missing_library_file(self):
        self.run.return_value = cli_result(0, f"{ARCH_DIR}/libnvidia-cfg.so.570.148.08\n")
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libnvidia-cfg.so.570.148.08: missing"], self.details)

    def test_fails_on_corrupt_library_file(self):
        self.write_lib("libnvidia-nvvm.so.570.148.08", b"not an elf" * 500)
        self.run.return_value = cli_result(0, f"{ARCH_DIR}/libnvidia-nvvm.so.570.148.08\n")
        self.assertEqual(1, self.module.main())
        self.assertEqual(["libnvidia-nvvm.so.570.148.08: not loadable"], self.details)

    def test_ignores_placeholders_of_other_capabilities(self):
        self.write_lib("libGLX_nvidia.so.570.148.08", PLACEHOLDER_MARKER)
        self.write_lib("libnvidia-encode.so.570.148.08", b"")
        self.run.return_value = cli_result(
            0,
            f"{ARCH_DIR}/libGLX_nvidia.so.570.148.08\n"
            f"{ARCH_DIR}/libnvidia-encode.so.570.148.08\n"
            f"{ARCH_DIR}/{os.path.basename(self.real_lib)}\n",
        )
        with self.assertLogs(self.module.LOGGER, level="INFO") as logs:
            self.assertEqual(0, self.module.main())
        self.assertIn("listed 3 libraries, 1 belong to the compute and utility capabilities", "\n".join(logs.output))
        self.assertEqual([], self.details)

    def test_ignores_blank_cli_lines(self):
        self.run.return_value = cli_result(0, f"\n{ARCH_DIR}/{os.path.basename(self.real_lib)}\n\n")
        self.assertEqual(0, self.module.main())
        self.assertEqual([], self.details)

    def test_prefix_match_is_exact_on_library_stem(self):
        is_checked = self.module.is_checked_library
        self.assertTrue(is_checked("libcuda.so.570.148.08"))
        self.assertTrue(is_checked("libnvidia-ml.so.570.148.08"))
        self.assertFalse(is_checked("libnvidia-pkcs11.so.570.148.08"))
        self.assertFalse(is_checked("libnvidia-pkcs11-openssl3.so.570.148.08"))
        self.assertFalse(is_checked("libcudart.so.12"))
        self.assertFalse(is_checked("libnvidia-ml-fake.so.1"))
        self.assertFalse(is_checked("libnvidia-glcore.so.570.148.08"))

    def test_unexpected_exception_does_not_drain(self):
        self.cdll.side_effect = RuntimeError("boom")
        with self.assertLogs(self.module.LOGGER, level="ERROR") as logs:
            self.assertEqual(0, self.module.main())
        self.assertIn("Unhandled exception", "\n".join(logs.output))
        self.assertIn("boom", "\n".join(logs.output))
        self.assertEqual([], self.details)


if __name__ == "__main__":
    unittest.main(verbosity=2)
