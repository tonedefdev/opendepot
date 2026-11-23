#!/usr/bin/env python3
"""Run NVIDIA SkillSpector in static mode over a directory of skills and save one JSON report per skill.

Reports feed the jevharness -scanner-reports flag, which sends each skill's findings to Jev. Pinned to SkillSpector
commit 48e0f942ea1f5fdb7aa096a6f65a0adeda2ecc4e (2.12.0). Install from that commit, for example:
  uv tool install --python 3.13 git+https://github.com/NVIDIA/skillspector.git@48e0f942ea1f5fdb7aa096a6f65a0adeda2ecc4e

Usage:
  python3 skillspector_scan.py --corpus DIR --out DIR [--binary skillspector] [--workers 4]

Each skill directory is a subdirectory of --corpus that contains SKILL.md. Exit code 1 from SkillSpector means the
report rates the skill high risk, so it is not treated as a failure. A report is only accepted when the scanner
reports execution_successful, and the parse step in pkg/jev rejects reports that are not.
"""
import argparse
import pathlib
import subprocess
import sys
from concurrent.futures import ThreadPoolExecutor

TIMEOUT_SECONDS = 300


def scan(binary, skill_dir, out_dir):
    """Scan one skill. Returns (name, return code). Skips skills whose report already exists."""
    name = skill_dir.name
    report = out_dir / f"{name}.json"
    if report.exists():
        return name, 0

    proc = subprocess.run(
        [binary, "scan", str(skill_dir), "--no-llm", "--format", "json", "--output", str(report)],
        capture_output=True,
        text=True,
        timeout=TIMEOUT_SECONDS,
    )
    (out_dir / f"{name}.log").write_text(proc.stdout + proc.stderr)
    if proc.returncode in (0, 1) and not report.exists():
        # A clean or high-risk exit without a report is a failed scan, not a result.
        return name, -1

    return name, proc.returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--corpus", required=True, type=pathlib.Path)
    parser.add_argument("--out", required=True, type=pathlib.Path)
    parser.add_argument("--binary", default="skillspector")
    parser.add_argument("--workers", type=int, default=4)
    args = parser.parse_args()

    args.out.mkdir(parents=True, exist_ok=True)
    skills = sorted(p for p in args.corpus.iterdir() if (p / "SKILL.md").exists())
    with ThreadPoolExecutor(max_workers=args.workers) as pool:
        results = list(pool.map(lambda s: scan(args.binary, s, args.out), skills))

    failed = [(name, code) for name, code in results if code not in (0, 1)]
    print(f"scanned {len(results)} skills; high-risk exits (1): {sum(c == 1 for _, c in results)}; "
          f"failed: {len(failed)} {failed[:10]}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
