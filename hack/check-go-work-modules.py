#!/usr/bin/env python3
"""Fail when a go.work module path is unsafe to interpolate into Makefile shell recipes."""

import json
import re
import subprocess
import sys

SAFE_MODULE_PATH = re.compile(r"(\./)?[A-Za-z0-9._][A-Za-z0-9._/-]*")


def main():
    result = subprocess.run(
        ["go", "work", "edit", "-json"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        sys.stderr.write(f"error: go work edit -json failed: {result.stderr.strip()}\n")
        return 1

    paths = [entry["DiskPath"] for entry in json.loads(result.stdout)["Use"]]
    unsafe = [path for path in paths if not SAFE_MODULE_PATH.fullmatch(path)]
    if unsafe:
        sys.stderr.write(
            "error: unsafe go.work module path(s): "
            + ", ".join(repr(path) for path in unsafe)
            + "\n"
            + "module paths may only contain letters, digits, '.', '_', '-', and '/'\n"
        )
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())
