import os
import pty
import select
import shlex
import shutil
import subprocess
import tempfile
import time
import unittest
from pathlib import Path


SCRIPT_DIR = Path(__file__).parent
WARNING = "Warning: This login node is outdated."
PROMPT = "__SOPERATOR_TEST_PROMPT__ "


class Terminal:
    def __init__(self, command, env):
        self.master, slave = pty.openpty()
        self.process = subprocess.Popen(
            command, stdin=slave, stdout=slave, stderr=slave, env=env,
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
        self.state = self.root / "state"
        self.state.mkdir()
        self.announcement = self.state / "announced-at"
        self.schedule = self.state / "scheduled-at"
        self.hook = self.root / "login_warning.sh"
        self.profile = self.root / "login_warning_profile.sh"
        for name in ("login_warning.sh", "login_warning_profile.sh"):
            source = (SCRIPT_DIR / name).read_text()
            source = source.replace("/run/soperator-login-state", str(self.state))
            source = source.replace("/usr/lib/soperator/login_warning.sh", str(self.hook))
            (self.root / name).write_text(source)
        self.hook.chmod(0o755)
        self.env = {
            "PATH": os.environ["PATH"], "HOME": str(self.root),
            "TERM": "dumb", "TZ": "Europe/Amsterdam",
        }
        self.bashes = [shutil.which("bash", path=self.env["PATH"])]
        system_bash = Path("/bin/bash")
        if system_bash.exists() and self.bashes[0] and system_bash.resolve() != Path(self.bashes[0]).resolve():
            self.bashes.insert(0, str(system_bash))

    def announce_update(self):
        # Downward API files have no trailing newline.
        self.announcement.write_text("2026-11-01T00:00:00Z")
        self.schedule.write_text("2026-12-01T00:00:00.123456789Z")

    def terminal(self, executable, login=False):
        home = Path(tempfile.mkdtemp(dir=self.root))
        startup = (
            f"export PATH={shlex.quote(self.env['PATH'])}\n"
            f". {shlex.quote(str(self.profile))}\nPS1={shlex.quote(PROMPT)}\n"
        )
        (home / ".bashrc").write_text(startup)
        (home / ".bash_profile").write_text(f". {shlex.quote(str(home / '.bashrc'))}\n")
        args = ["--login", "-i"] if login else ["--noprofile", "--rcfile", str(home / ".bashrc"), "-i"]
        terminal = Terminal([executable, *args], {**self.env, "HOME": str(home)})
        self.addCleanup(terminal.close)
        return terminal

    def test_interactive_login_and_non_login_shells_warn(self):
        self.announce_update()
        for executable in self.bashes:
            for login in (False, True):
                with self.subTest(executable=executable, login=login):
                    if executable is None:
                        self.skipTest("Bash is not installed")
                    terminal = self.terminal(executable, login=login)
                    output = terminal.read_prompt()
                    self.assertEqual(1, output.count(WARNING))
                    self.assertIn("Scheduled restart: at or after 2026-12-01 01:00:00 CET.", output)
                    self.assertNotIn(WARNING, terminal.command("true"))
                    exported = self.root / "prompt-command"
                    terminal.command(
                        'export PROMPT_COMMAND="history -a; $PROMPT_COMMAND"; '
                        f'printf "%s" "$PROMPT_COMMAND" > {shlex.quote(str(exported))}'
                    )
                    worker = Terminal(
                        [executable, "--noprofile", "--norc", "-i"],
                        {**self.env, "PROMPT_COMMAND": exported.read_text(), "PS1": PROMPT},
                    )
                    self.addCleanup(worker.close)
                    for output in (worker.read_prompt(), worker.command("true")):
                        self.assertNotIn(WARNING, output)
                        self.assertNotIn("command not found", output)

    def test_existing_terminal_detects_update_at_next_prompt(self):
        for executable in self.bashes:
            with self.subTest(executable=executable):
                if executable is None:
                    self.skipTest("Bash is not installed")
                self.announcement.unlink(missing_ok=True)
                self.schedule.unlink(missing_ok=True)
                terminal = self.terminal(executable)
                self.assertNotIn(WARNING, terminal.read_prompt())
                self.announce_update()
                self.assertEqual(1, terminal.command("true").count(WARNING))
                self.assertNotIn(WARNING, terminal.command("true"))

    def test_noninteractive_commands_are_silent_with_or_without_pty(self):
        self.announce_update()
        command = f". {shlex.quote(str(self.profile))}; printf 'result\\n'; exit 7"
        for executable in self.bashes:
            with self.subTest(executable=executable):
                if executable is None:
                    self.skipTest("Bash is not installed")
                args = [executable, "--noprofile", "--norc", "-c", command]
                result = subprocess.run(args, env=self.env, capture_output=True, text=True, check=False)
                self.assertEqual(7, result.returncode)
                self.assertEqual("result\n", result.stdout)
                self.assertEqual("", result.stderr)

                master, slave = pty.openpty()
                try:
                    result = subprocess.run(
                        args, env=self.env, stdin=slave, stdout=slave, stderr=slave, check=False,
                    )
                    self.assertEqual(7, result.returncode)
                    self.assertTrue(select.select([master], [], [], 10)[0], "Command did not produce output")
                    self.assertEqual(b"result\r\n", os.read(master, 65536))
                finally:
                    os.close(master)
                    os.close(slave)


if __name__ == "__main__":
    unittest.main(verbosity=2)
