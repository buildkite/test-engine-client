#!/usr/bin/env python3
"""Generate or check the CLI-only source tree for the legacy Go module."""

import argparse
import re
import stat
import subprocess
from pathlib import Path

CANONICAL = b"github.com/buildkite/bktest/v3"
LEGACY = b"github.com/buildkite/test-engine-client/v3"
MANIFEST = Path(__file__).with_name("manifest")


def mappings():
    for line in MANIFEST.read_text().splitlines():
        if line and not line.startswith("#"):
            yield line.split("\t")


def expected_tree(repo, commit):
    def git(*args):
        return subprocess.check_output(["git", "-C", repo, *args])

    if not re.fullmatch(r"[0-9a-f]{40}", commit):
        raise ValueError("pass the exact canonical commit SHA, not a ref")
    if git("cat-file", "-t", commit) != b"commit\n":
        raise ValueError(f"not a commit: {commit}")

    tree = {
        ".bktest-source": (0o644, f"repository github.com/buildkite/bktest\ncommit {commit}\n".encode()),
    }
    for source, destination in mappings():
        entries = git("ls-tree", "-rz", "--full-tree", commit, "--", source)
        if not entries:
            raise ValueError(f"manifest path missing at {commit}: {source}")
        for entry in entries.rstrip(b"\0").split(b"\0"):
            metadata, name = entry.split(b"\t", 1)
            mode, kind, oid = metadata.decode().split()
            if kind != "blob" or mode not in {"100644", "100755"}:
                raise ValueError(f"unsupported Git entry: {source}: {mode} {kind}")
            if source.endswith("/"):
                name = name.decode().removeprefix(source)
                target = (Path(destination) / name).as_posix()
            else:
                if name.decode() != source:
                    raise ValueError(f"manifest expected a file: {source}")
                target = destination
            if target in tree:
                raise ValueError(f"duplicate projected path: {target}")
            data = git("cat-file", "blob", oid)
            if target == "go.mod":
                module = b"module " + CANONICAL + b"\n"
                if not data.startswith(module):
                    raise ValueError("unexpected canonical module declaration")
                data = b"module " + LEGACY + b"\n" + data[len(module):]
            elif target.endswith(".go"):
                data = data.replace(b'"' + CANONICAL + b"/internal/", b'"' + LEGACY + b"/internal/")
            tree[target] = (int(mode[-3:], 8), data)
    return tree


def committed_projection(repo, expected):
    def git(*args):
        return subprocess.check_output(["git", "-C", repo, *args])

    subtrees = tuple(dst for _, dst in mappings() if dst.endswith("/") and dst != "./")
    actual = {}
    for entry in git("ls-tree", "-rz", "HEAD").split(b"\0"):
        if not entry:
            continue
        metadata, name = entry.split(b"\t", 1)
        path = name.decode()
        if path not in expected and not path.startswith(subtrees) and not ("/" not in path and path.endswith(".go")):
            continue
        mode, kind, oid = metadata.decode().split()
        if kind != "blob" or mode not in {"100644", "100755"}:
            raise ValueError(f"unsupported projected Git entry: {path}: {mode} {kind}")
        actual[path] = (int(mode[-3:], 8), git("cat-file", "blob", oid))
    return actual


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["generate", "check", "check-commit"])
    parser.add_argument("repo", type=Path, help="local canonical Git repository")
    parser.add_argument("commit", help="full canonical commit SHA")
    parser.add_argument("output", type=Path, help="standalone projection directory, or legacy repository for check-commit")
    args = parser.parse_args()

    try:
        expected = expected_tree(args.repo, args.commit)
        if args.action == "generate":
            args.output.mkdir(parents=True, exist_ok=False)
            for name, (mode, data) in expected.items():
                file = args.output / name
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_bytes(data)
                file.chmod(mode)
        elif args.action == "check":
            if not args.output.is_dir():
                raise ValueError(f"missing projection directory: {args.output}")
            actual = {}
            for file in args.output.rglob("*"):
                mode = file.lstat().st_mode
                if stat.S_ISDIR(mode):
                    continue
                if not stat.S_ISREG(mode):
                    raise ValueError(f"unexpected non-file: {file}")
                git_mode = 0o755 if mode & stat.S_IXUSR else 0o644
                actual[file.relative_to(args.output).as_posix()] = (git_mode, file.read_bytes())
        else:
            actual = committed_projection(args.output, expected)
        if args.action != "generate":
            for name in sorted(actual.keys() | expected.keys()):
                if name not in expected:
                    raise ValueError(f"unexpected projected file: {name}")
                if name not in actual:
                    raise ValueError(f"missing projected file: {name}")
                if actual[name] != expected[name]:
                    raise ValueError(f"projected bytes or executable bit differ: {name}")
        print(f"{args.action}: {len(expected)} files from {args.commit}")
    except (OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"{error}\n")


if __name__ == "__main__":
    main()
