from __future__ import annotations

import os
import subprocess
import tempfile
from pathlib import Path

import pytest


@pytest.mark.skipif(os.name != "posix", reason="shell integration test requires POSIX shell support")
def test_niimath_shell_example_script() -> None:
    if os.environ.get("CCX3_SHELL_INTEGRATION") != "1":
        pytest.skip("set CCX3_SHELL_INTEGRATION=1 to run the shell integration test")

    repo_root = Path(__file__).resolve().parents[1]
    with tempfile.TemporaryDirectory() as cache_home:
        env = os.environ.copy()
        env.setdefault("CCX3_CCVM", str(repo_root / "src" / "pyneurodesk" / "bin" / "ccvm"))
        env["XDG_CACHE_HOME"] = cache_home

        result = subprocess.run(
            ["bash", str(repo_root / "examples" / "test_niimath_shell.sh")],
            cwd=repo_root.parent,
            env=env,
            capture_output=True,
            text=True,
            check=False,
        )

        if result.returncode != 0:
            pytest.fail(
                "shell integration script failed\n"
                f"stdout:\n{result.stdout}\n"
                f"stderr:\n{result.stderr}"
            )
