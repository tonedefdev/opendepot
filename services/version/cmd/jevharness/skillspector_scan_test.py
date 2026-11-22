import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import skillspector_scan


def completed(code, report=None):
    """Returns a subprocess.run stand-in that exits with code and writes report to the --output path."""

    def run(argv, **kwargs):
        if report is not None:
            out = pathlib.Path(argv[argv.index("--output") + 1])
            out.write_text(report)

        return subprocess.CompletedProcess(argv, code, stdout="", stderr="")

    return run


class ScanTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.corpus = pathlib.Path(self.tmp.name) / "corpus"
        self.skill = self.corpus / "demo"
        self.skill.mkdir(parents=True)
        (self.skill / "SKILL.md").write_text("---\nname: demo\n---\n")
        self.out = pathlib.Path(self.tmp.name) / "out"
        self.out.mkdir()

    def test_clean_exit_with_report_is_success(self):
        with mock.patch("subprocess.run", side_effect=completed(0, '{"execution_successful": true}')) as run:
            name, code = skillspector_scan.scan("skillspector", self.skill, self.out)

        self.assertEqual((name, code), ("demo", 0))
        argv = run.call_args.args[0]
        self.assertEqual(argv[:3], ["skillspector", "scan", str(self.skill)])
        self.assertFalse(run.call_args.kwargs.get("shell", False))
        self.assertTrue((self.out / "demo.json").exists())

    def test_high_risk_exit_with_report_is_kept(self):
        with mock.patch("subprocess.run", side_effect=completed(1, '{"execution_successful": true}')):
            name, code = skillspector_scan.scan("skillspector", self.skill, self.out)

        self.assertEqual((name, code), ("demo", 1))

    def test_high_risk_exit_without_report_is_a_failure(self):
        with mock.patch("subprocess.run", side_effect=completed(1)):
            name, code = skillspector_scan.scan("skillspector", self.skill, self.out)

        self.assertEqual((name, code), ("demo", -1))

    def test_clean_exit_without_report_is_a_failure(self):
        with mock.patch("subprocess.run", side_effect=completed(0)):
            _, code = skillspector_scan.scan("skillspector", self.skill, self.out)

        self.assertEqual(code, -1)

    def test_error_exit_is_returned(self):
        with mock.patch("subprocess.run", side_effect=completed(2)):
            _, code = skillspector_scan.scan("skillspector", self.skill, self.out)

        self.assertEqual(code, 2)

    def test_existing_report_is_skipped(self):
        (self.out / "demo.json").write_text("{}")
        with mock.patch("subprocess.run") as run:
            name, code = skillspector_scan.scan("skillspector", self.skill, self.out)

        self.assertEqual((name, code), ("demo", 0))
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
