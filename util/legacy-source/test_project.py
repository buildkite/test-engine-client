import subprocess
import tempfile
import unittest
from pathlib import Path

TOOLS = Path(__file__).resolve().parent
CANONICAL = "github.com/buildkite/bktest/v3"
LEGACY = "github.com/buildkite/test-engine-client/v3"


class ProjectionTest(unittest.TestCase):
    def test_projects_committed_cli_only_and_checks_bytes_and_modes(self):
        # Keep scratch files out of both the Git fixture and checked projection trees.
        with tempfile.TemporaryDirectory() as temp:
            temp = Path(temp)
            repo = temp / "source"
            repo.mkdir()

            def write(name, data):
                file = repo / name
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text(data)
                return file

            def git(*args):
                return subprocess.check_output(["git", "-C", repo, *args], text=True).strip()

            for line in (TOOLS / "manifest").read_text().splitlines():
                if line and not line.startswith("#"):
                    source = line.split("\t")[0]
                    write(source + ("fixture.txt" if source.endswith("/") else ""), "fixture\n")
            write("go.mod", f"module {CANONICAL}\n\ngo 1.27.3\n")
            entry = write("cmd/bktest/main.go", f'package main\n\nimport "{CANONICAL}/internal/version"\n')
            write("internal/runner/testdata/run.sh", "#!/bin/sh\n").chmod(0o755)
            write("test-collector-ruby/collector.rb", "collector\n")
            write("internal/collector/main.go", "package collector\n")
            write("docs/cli-import.md", "canonical import runbook\n")
            write(".buildkite/pipeline.cli-release.yml", "release configuration\n")
            write(".gitattributes", "cmd/bktest/main.go export-ignore\n")
            git("init", "-q")
            git("add", ".")
            git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "fixture")
            commit = git("rev-parse", "HEAD")
            entry.write_text("uncommitted work\n")

            def project(action, path):
                return subprocess.run(
                    ["python3", TOOLS / "project.py", action, repo, commit, path],
                    capture_output=True, text=True,
                )

            first, second = temp / "first", temp / "second"
            for output in (first, second):
                result = project("generate", output)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual((output / "main.go").read_text(), f'package main\n\nimport "{LEGACY}/internal/version"\n')
                self.assertEqual((output / "go.mod").read_text(), f"module {LEGACY}\n\ngo 1.27.3\n")
                self.assertEqual((output / ".bktest-source").read_text(), f"repository github.com/buildkite/bktest\ncommit {commit}\n")
                for omitted in ("cmd", "test-collector-ruby", "internal/collector", "docs/cli-import.md", ".buildkite", ".gitattributes"):
                    self.assertFalse((output / omitted).exists(), omitted)
            subprocess.run(["diff", "-r", first, second], check=True)
            result = project("check", second)
            self.assertEqual(result.returncode, 0, result.stderr)
            (second / "internal/runner/testdata/run.sh").chmod(0o644)
            self.assertIn("executable bit differ", project("check", second).stderr)
            (second / "internal/runner/testdata/run.sh").chmod(0o755)
            (second / "collector.go").touch()
            self.assertIn("unexpected projected file: collector.go", project("check", second).stderr)

            def legacy(*args):
                return subprocess.check_output(["git", "-C", first, *args])

            def check_commit():
                return project("check-commit", first)

            legacy("init", "-q")
            (first / "README.md").write_text("Owned by the legacy repository\n")
            legacy("add", ".")
            legacy("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "projection")
            executable = "internal/runner/testdata/run.sh"
            (first / executable).chmod(0o644)
            result = check_commit()
            self.assertEqual(result.returncode, 0, result.stderr)
            legacy("update-index", "--chmod=-x", executable)
            legacy("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "commit", "-qm", "mode drift")
            (first / executable).chmod(0o755)
            result = check_commit()
            self.assertEqual(result.returncode, 1)
            self.assertIn(f"projected bytes or executable bit differ: {executable}", result.stderr)


if __name__ == "__main__":
    unittest.main()
