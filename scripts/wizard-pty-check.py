#!/usr/bin/env python3
"""Unix PTY smoke check. Build bin/pluto first; no runtime services required."""
from pathlib import Path
import fcntl
import os
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

BINARY = str(Path(__file__).resolve().parent.parent / "bin/pluto")


def session(arguments, mode):
    with tempfile.TemporaryDirectory(prefix="pluto-wizard-pty-") as temporary:
        root = Path(temporary).resolve()
        master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
        before = termios.tcgetattr(slave)
        child = subprocess.Popen(
            [BINARY, *arguments],
            cwd=root,
            stdin=slave,
            stdout=slave,
            stderr=slave,
            env={**os.environ, "TERM": "xterm-256color", "CI": ""},
            start_new_session=True,
        )
        output = bytearray()

        def read_available(timeout=0.1):
            if select.select([master], [], [], timeout)[0]:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    return False
            return True

        def until(fragment, seconds=8):
            deadline = time.monotonic() + seconds
            while fragment not in output:
                if time.monotonic() > deadline:
                    raise AssertionError((fragment, bytes(output[-1200:])))
                if not read_available() or child.poll() is not None:
                    break
            assert fragment in output, (fragment, bytes(output[-1200:]))

        try:
            until(b"LAUNCH CONTROL")
            if mode == "cancel":
                os.write(master, b"\x1b")
            elif mode == "ctrlc":
                os.write(master, b"\x03")
            elif mode == "signal":
                child.send_signal(signal.SIGTERM)
            else:
                if mode == "menu":
                    os.write(master, b"\r")
                for number in range(1, 13):
                    until(f"{number}/12".encode())
                    if mode == "menu" and number == 1:
                        os.write(master, b"\x15mission")
                    if mode == "menu" and number == 12:
                        os.write(master, b"\x1b[B")  # Select preview-only.
                    os.write(master, b"\r")
                until(b"Review your choices")
                os.write(master, b"\r")
                until(b"Equivalent command:")

            # Keep draining the PTY: the dry-run file list can fill its buffer.
            deadline = time.monotonic() + 10
            while child.poll() is None:
                if time.monotonic() > deadline:
                    raise AssertionError(("exit timeout", bytes(output[-1000:])))
                if not read_available(0.05):
                    break
            code = child.wait(timeout=2)
            while select.select([master], [], [], 0.05)[0]:
                if not read_available(0):
                    break

            complete = mode in ("complete", "menu")
            assert code == (0 if complete else 1), (code, bytes(output[-1000:]))
            after = termios.tcgetattr(slave)
            # macOS may set transient PENDIN when returning to cooked input.
            after[3] &= ~getattr(termios, "PENDIN", 0)
            before[3] &= ~getattr(termios, "PENDIN", 0)
            assert after == before, ("terminal not restored", before, after)
            assert list(root.iterdir()) == [], "unexpected writes"
            if complete:
                assert b"--dry-run=true" in output
            print(f"PTY {mode}: passed; terminal restored; no writes", flush=True)
        finally:
            if child.poll() is None:
                child.kill()
                child.wait()
            os.close(master)
            os.close(slave)


session([], "cancel")
session(["init"], "signal")
session(
    [
        "--interactive", "init", "--name", "mission", "--module",
        "example.com/mission", "--no-mise", "--no-fnox", "--no-docker", "--dry-run",
    ],
    "complete",
)
session(["init"], "ctrlc")
session([], "menu")
