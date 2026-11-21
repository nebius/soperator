import os
import pty
import select
import shlex
import subprocess
import tempfile
import time
import unittest
from pathlib import Path


SCRIPT_DIR = Path(__file__).parent
WARNING = "Warning: This login host is outdated."
PROMPT = "__SOPERATOR_TEST_PROMPT__ "


class Terminal:
    def __init__(self, args, env):
        self.master, slave = pty.openpty()
        self.process = subprocess.Popen(
            ["bash", *args], stdin=slave, stdout=slave, stderr=slave, env=env,
        )
        os.close(slave)

    def read_prompt(self):
        output = b""
        deadline = time.monotonic() + 10
        while PROMPT.encode() not in output:
            remaining = deadline - time.monotonic()
            if remaining <= 0 or not select.select([self.master], [], [], remaining)[0]:
                raise AssertionError(f"Shell did not display a prompt: {output!r}")
            output += os.read(self.master, 65536)
        return output.decode()

    def command(self, command):
        os.write(self.master, (command + "\n").encode())
        return self.read_prompt()

    def close(self):
        if self.process.poll() is None:
            self.process.kill()
            self.process.wait(timeout=3)
        os.close(self.master)


class LoginWarningTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.announcement = self.root / "announced-at"
        self.schedule = self.root / "scheduled-at"
        self.hook = self.root / "login_warning.sh"
        self.hook.write_text((SCRIPT_DIR / "login_warning.sh").read_text().replace(
            "/run/soperator-login-state", shlex.quote(str(self.root)),
        ))
        self.env = {"PATH": os.environ["PATH"], "HOME": str(self.root), "TERM": "dumb"}

    def announce_update(self):
        # Downward API files have no trailing newline.
        self.announcement.write_text("2026-11-01T00:00:00Z")
        self.schedule.write_text("2026-12-01T00:00:00Z")

    def terminal(self, login=False):
        rc = self.root / "bashrc"
        rc.write_text(f". {shlex.quote(str(self.hook))}\nPS1={shlex.quote(PROMPT)}\n")
        if login:
            (self.root / ".bash_profile").write_text(f". {shlex.quote(str(rc))}\n")
            args = ["--login", "-i"]
        else:
            args = ["--noprofile", "--rcfile", str(rc), "-i"]
        terminal = Terminal(args, self.env)
        self.addCleanup(terminal.close)
        return terminal

    def test_interactive_login_and_non_login_shells_warn(self):
        self.announce_update()
        for login in (False, True):
            with self.subTest(login=login):
                terminal = self.terminal(login=login)
                self.assertEqual(1, terminal.read_prompt().count(WARNING))

    def test_existing_terminal_detects_update_at_next_prompt(self):
        terminal = self.terminal()
        self.assertNotIn(WARNING, terminal.read_prompt())
        self.announce_update()
        self.assertEqual(1, terminal.command(":").count(WARNING))

    def test_noninteractive_commands_are_silent_with_or_without_pty(self):
        self.announce_update()
        command = f". {shlex.quote(str(self.hook))}; printf 'result\\n'; exit 7"
        result = subprocess.run(
            ["bash", "--noprofile", "--norc", "-c", command], env=self.env,
            capture_output=True, text=True, check=False,
        )
        self.assertEqual(7, result.returncode)
        self.assertEqual("result\n", result.stdout)
        self.assertEqual("", result.stderr)

        master, slave = pty.openpty()
        try:
            result = subprocess.run(
                ["bash", "--noprofile", "--norc", "-c", command], env=self.env,
                stdin=slave, stdout=slave, stderr=slave, check=False,
            )
            self.assertEqual(7, result.returncode)
            self.assertTrue(select.select([master], [], [], 10)[0], "Command did not produce output")
            self.assertEqual(b"result\r\n", os.read(master, 65536))
        finally:
            os.close(master)
            os.close(slave)


if __name__ == "__main__":
    unittest.main(verbosity=2)
