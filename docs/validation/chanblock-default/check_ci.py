# pyright: strict
"""Verify the frozen default-select regression and the exact pinned chi replay."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import time
from collections import Counter
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import NoReturn, cast

DIRECTORY = "docs/validation/chanblock-default"
MANIFEST_SHA256 = "e9290123608d8cae5f929bfa6b6b635f8b5764270cfafec22ad497bd78f4b8ca"
ANALYZER = "github.com/gosuda/gglint/internal/analyzers/chanblock"
RULES = "github.com/gosuda/gglint/internal/rules"
ROOT = "TestDefaultSelectBounds"
NEGATIVE = f"{ROOT}/communications"
ANALYZER_NAMES = frozenset(
    ("TestAnalyzer", ROOT)
    + tuple(
        f"{ROOT}/{name}"
        for name in (
            "communications",
            "operands",
            "bodies",
            "accounting",
            "cancellation",
        )
    )
)
PARITY_NAMES = frozenset(
    ("TestLogosPorts", "TestLogosPorts/correctness_unbounded_channel_op")
)
CHI_REVISION = "c64ed7d6635f33a8b2feaff7dcfa55677023ac7d"
CHI_SUM = "h1:oMQs0ebb+20GDsR632aZraCgKic1nf5stVTvz73euoM="
GO_VERSION = "go version go1.27.4 linux/amd64\n"
CRASH = re.compile(
    r"(?i:\b(?:panic:|fatal error:|signal:)|test timed out)|\bSIG[A-Z]{2,}\b"
)
MAX_OUTPUT = 16 * 1024 * 1024
ENVIRONMENT_KEYS = (
    "GOFLAGS",
    "GOENV",
    "GOWORK",
    "GOTOOLCHAIN",
    "GOPROXY",
    "GOSUMDB",
    "GOTELEMETRY",
    "GOMAXPROCS",
    "GOOS",
    "GOARCH",
    "GOAMD64",
    "CGO_ENABLED",
    "GOMEMLIMIT",
    "GOGC",
    "GOCACHE",
    "GOMODCACHE",
    "PATH",
    "GOROOT",
    "GOEXPERIMENT",
    "GODEBUG",
)


def reject_constant(value: str) -> NoReturn:
    raise ValueError(f"non-JSON numeric constant {value}")


def unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key {key!r}")
        result[key] = value
    return result


def decode(text: str) -> object:
    return json.loads(
        text, object_pairs_hook=unique_object, parse_constant=reject_constant
    )


def mapping(value: object) -> dict[str, object]:
    if not isinstance(value, dict):
        raise TypeError("expected a JSON object")
    return cast(dict[str, object], value)


def string(value: object) -> str:
    if not isinstance(value, str):
        raise TypeError("expected a string")
    return value


def digest(path: Path) -> str:
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def write_json(path: Path, value: object) -> None:
    path.write_text(
        json.dumps(value, indent=2, sort_keys=True) + "\n", encoding="utf-8"
    )


def rooted(root: Path, relative: str) -> Path:
    path = Path(relative)
    if path.is_absolute() or ".." in path.parts or not relative or "\n" in relative:
        raise ValueError(f"invalid relative input path: {relative!r}")
    target = root / path
    if (
        target.is_symlink()
        or not target.resolve(strict=True).is_relative_to(root)
        or not target.is_file()
    ):
        raise ValueError(f"input is not a regular file inside {root}: {relative}")
    return target


def verify_frozen(repo: Path) -> dict[str, str]:
    manifest = rooted(repo, f"{DIRECTORY}/FROZEN.sha256")
    if digest(manifest) != MANIFEST_SHA256:
        raise ValueError("frozen manifest hash differs")
    files: dict[str, str] = {}
    for line in manifest.read_text(encoding="utf-8").splitlines():
        expected, separator, relative = line.partition("  ")
        if (
            not separator
            or re.fullmatch(r"[0-9a-f]{64}", expected) is None
            or relative in files
        ):
            raise ValueError("malformed or duplicate frozen manifest entry")
        actual = digest(rooted(repo, relative))
        if actual != expected:
            raise ValueError(f"frozen input differs: {relative}")
        files[relative] = actual
    if len(files) != 32:
        raise ValueError("frozen inventory must contain exactly 32 files")
    return files


def expected_diagnostics() -> Counter[str]:
    messages = {
        "send": "channel send can block forever: select on ctx.Done() or use a buffered channel",
        "receive": "channel receive can block forever: select on ctx.Done()",
    }
    locations = (
        (5, 7, "send"),
        (12, 7, "receive"),
        (19, 16, "receive"),
        (28, 16, "receive"),
        (38, 7, "send"),
        (39, 7, "receive"),
        (47, 7, "send"),
        (57, 16, "receive"),
    )
    return Counter(
        f"defaultselect/communications/communications.go:{line}:{column}: unexpected diagnostic: {messages[kind]}"
        for line, column, kind in locations
    )


def check_test_output(
    stdout: str,
    stderr: bytes,
    returncode: int,
    package: str,
    names: frozenset[str],
    *,
    negative: bool = False,
) -> None:
    if stderr:
        raise ValueError("test command wrote stderr")
    active: dict[str, str] = {}
    runs: Counter[str] = Counter()
    results: Counter[str] = Counter()
    diagnostics: Counter[str] = Counter()
    started = False
    finished = False
    outcome = "fail" if negative else "pass"
    for line in stdout.splitlines():
        event = mapping(decode(line))
        action = string(event.get("Action"))
        if action.startswith("build-") or "FailedBuild" in event:
            raise ValueError("build event cannot establish test evidence")
        if set(event) - {
            "Time",
            "Action",
            "Package",
            "Test",
            "Elapsed",
            "Output",
            "OutputType",
        }:
            raise ValueError("unknown test event field")
        if ("Output" in event or "OutputType" in event) and action != "output":
            raise ValueError("output attached to a non-output event")
        if "OutputType" in event and string(event["OutputType"]) not in (
            "",
            "frame",
            "error",
            "error-continue",
        ):
            raise ValueError("unknown output classification")
        if event.get("Package") != package:
            raise ValueError("unexpected package")
        if finished:
            raise ValueError("event after package completion")
        name = string(event.get("Test", ""))
        if "Test" in event and not name:
            raise ValueError("empty named-test identity")
        if name and name not in names:
            raise ValueError(f"unexpected named test: {name}")
        if action == "start":
            if started or name:
                raise ValueError("duplicate or named package start")
            started = True
            continue
        if not started:
            raise ValueError("event before package start")
        if action == "output":
            if name and name not in active:
                raise ValueError("output outside test lifetime")
            output = string(event.get("Output"))
            if CRASH.search(output):
                raise ValueError("crash or timeout output")
            if negative:
                for text in output.splitlines():
                    match = re.fullmatch(
                        r"\s+\S+\.go:[1-9][0-9]*: (defaultselect/communications/communications\.go:.*)",
                        text,
                    )
                    if match:
                        diagnostic = match.group(1)
                        if name != NEGATIVE or diagnostic not in expected_diagnostics():
                            raise ValueError("unexpected negative diagnostic or owner")
                        diagnostics[diagnostic] += 1
                    elif (
                        re.fullmatch(
                            r"\s*(?:=== (?:RUN|PAUSE|CONT|NAME)\s+\S+|--- FAIL: \S+ \([0-9.]+s\)|FAIL(?:\s+\S+\s+[0-9.]+s)?)",
                            text,
                        )
                        is None
                    ):
                        raise ValueError(f"unrecognized negative output: {text!r}")
            continue
        if not name:
            if action != outcome or active:
                raise ValueError("incorrect package result or active tests remain")
            finished = True
        elif action == "run":
            parent = name.rpartition("/")[0]
            if runs[name] or (parent in names and active.get(parent) != "running"):
                raise ValueError("duplicate run or child outside running parent")
            runs[name] += 1
            active[name] = "running"
        elif action in ("pause", "cont"):
            before, after = (
                ("running", "paused") if action == "pause" else ("paused", "running")
            )
            if active.get(name) != before:
                raise ValueError("invalid pause/continue transition")
            active[name] = after
        elif action == outcome:
            if active.get(name) != "running" or any(
                child.startswith(name + "/") for child in active
            ):
                raise ValueError("test result outside completed test lifetime")
            del active[name]
            results[name] += 1
        else:
            raise ValueError(f"unexpected action {action!r}")
    if not finished or runs != Counter(names) or results != Counter(names):
        raise ValueError("incomplete or duplicated named test evidence")
    if returncode != (1 if negative else 0):
        raise ValueError("exit status differs from exact expected outcome")
    if negative and diagnostics != expected_diagnostics():
        raise ValueError("negative lacks the exact eight unexpected diagnostics")


def check_cli_output(
    stdout: str,
    stderr: bytes,
    returncode: int,
    mode: str,
    config: str,
    expected: object,
) -> None:
    result = mapping(decode(stdout))
    if (
        set(result) != {"mode", "config", "findings"}
        or result["mode"] != mode
        or result["config"] != config
    ):
        raise ValueError("CLI envelope or invocation differs")
    if json.dumps(result["findings"], sort_keys=True) != json.dumps(
        expected, sort_keys=True
    ):
        raise ValueError("CLI findings differ from the exact frozen array")
    if returncode != 1 or stderr != f"gglint: config {config}\n".encode():
        raise ValueError("CLI status or stderr differs")


def git_value(repo: Path, *args: str) -> str:
    return subprocess.check_output(
        ("git", "-C", str(repo), *args), text=True, timeout=30
    ).strip()


def tracked_files(root: Path) -> tuple[str, ...]:
    data = subprocess.check_output(
        ("git", "-C", str(root), "ls-files", "-z"), timeout=30
    )
    files = tuple(data.decode().removesuffix("\0").split("\0"))
    if not files or len(set(files)) != len(files):
        raise ValueError("empty or duplicate tracked file inventory")
    return files


def source_identity(repo: Path, expected_head: str) -> dict[str, object]:
    if git_value(repo, "rev-parse", "HEAD") != expected_head or git_value(
        repo, "status", "--porcelain=v1"
    ):
        raise ValueError("source revision differs or worktree is dirty")
    hashes = {name: digest(rooted(repo, name)) for name in sorted(tracked_files(repo))}
    return {
        "head": expected_head,
        "tree": git_value(repo, "rev-parse", "HEAD^{tree}"),
        "sha256": hashes,
    }


def tool_identity(go: Path) -> dict[str, dict[str, str]]:
    paths = {"go": go, "python": Path(sys.executable).resolve(strict=True)}
    for name in ("compile", "link", "asm", "vet"):
        paths[name] = go.parent.parent / "pkg/tool/linux_amd64" / name
    return {
        name: {"path": str(path), "sha256": digest(path)}
        for name, path in paths.items()
    }


@dataclass(frozen=True)
class Run:
    command: tuple[str, ...]
    cwd: str
    returncode: int | None
    timed_out: bool
    error: str
    seconds: float
    stdout: str
    stderr: str
    stdout_sha256: str
    stderr_sha256: str


def capture(
    command: tuple[str, ...],
    cwd: Path,
    env: dict[str, str],
    out: Path,
    label: str,
    *,
    timeout: int = 660,
) -> Run:
    stdout, stderr = out / f"{label}.stdout", out / f"{label}.stderr"
    started = time.monotonic()
    code: int | None = None
    timed_out = False
    error = ""
    with stdout.open("xb") as output, stderr.open("xb") as errors:
        try:
            process = subprocess.Popen(
                command,
                cwd=cwd,
                env=env,
                stdout=output,
                stderr=errors,
                start_new_session=True,
            )
            while True:
                try:
                    code = process.wait(timeout=0.25)
                    break
                except subprocess.TimeoutExpired:
                    timed_out = time.monotonic() - started >= timeout
                    if max(stdout.stat().st_size, stderr.stat().st_size) > MAX_OUTPUT:
                        error = "command output exceeds the verification limit"
                    if timed_out or error:
                        os.killpg(process.pid, signal.SIGKILL)
                        code = process.wait(timeout=30)
                        break
        except OSError as exc:
            error = str(exc)
    run = Run(
        command,
        str(cwd),
        code,
        timed_out,
        error,
        round(time.monotonic() - started, 3),
        str(stdout),
        str(stderr),
        digest(stdout),
        digest(stderr),
    )
    write_json(out / f"{label}.run.json", asdict(run))
    return run


def streams(run: Run) -> tuple[str, bytes, int]:
    if run.timed_out or run.error or run.returncode is None:
        raise ValueError("command did not complete normally")
    stdout, stderr = Path(run.stdout), Path(run.stderr)
    if max(stdout.stat().st_size, stderr.stat().st_size) > MAX_OUTPUT:
        raise ValueError("command output exceeds the verification limit")
    if digest(stdout) != run.stdout_sha256 or digest(stderr) != run.stderr_sha256:
        raise ValueError("raw command output changed")
    return stdout.read_text(encoding="utf-8"), stderr.read_bytes(), run.returncode


def require_success(run: Run, *, quiet: bool = False) -> str:
    stdout, stderr, code = streams(run)
    if (
        code != 0
        or (quiet and (stdout or stderr))
        or CRASH.search(stdout + stderr.decode())
    ):
        raise ValueError("preparation/build command failed or wrote unexpected output")
    return stdout


def tree_sum(root: Path, files: tuple[str, ...]) -> str:
    actual: set[str] = set()
    for path in root.rglob("*"):
        relative = path.relative_to(root)
        if relative.parts[0] == ".git":
            continue
        if path.is_symlink():
            raise ValueError("source contains a symlink")
        if path.is_file():
            actual.add(relative.as_posix())
    if actual != set(files):
        raise ValueError("source file inventory changed")
    hashed = hashlib.sha256()
    for name in sorted(files):
        hashed.update(f"{digest(rooted(root, name))}  {name}\n".encode())
    return "h1:" + base64.b64encode(hashed.digest()).decode("ascii")


def prepare_chi(out: Path, env: dict[str, str]) -> tuple[Path, tuple[str, ...]]:
    source = out / "chi-primary"
    git_env = env | {
        "GIT_CONFIG_NOSYSTEM": "1",
        "GIT_CONFIG_GLOBAL": "/dev/null",
        "GIT_TERMINAL_PROMPT": "0",
    }
    for key in tuple(git_env):
        if key.startswith("GIT_") and key not in {
            "GIT_CONFIG_NOSYSTEM",
            "GIT_CONFIG_GLOBAL",
            "GIT_TERMINAL_PROMPT",
        }:
            del git_env[key]
    commands = (
        ("git", "init", "--quiet", str(source)),
        (
            "git",
            "-C",
            str(source),
            "fetch",
            "--no-tags",
            "--depth=1",
            "https://github.com/go-chi/chi.git",
            CHI_REVISION,
        ),
        ("git", "-C", str(source), "checkout", "--quiet", "--detach", CHI_REVISION),
    )
    for index, command in enumerate(commands):
        require_success(
            capture(command, out, git_env, out, f"chi-source-{index}", timeout=180)
        )
    if git_value(source, "rev-parse", "HEAD") != CHI_REVISION or git_value(
        source, "status", "--porcelain=v1"
    ):
        raise ValueError("chi checkout differs from the frozen revision")
    files = tracked_files(source)
    if tree_sum(source, files) != CHI_SUM:
        raise ValueError("chi source digest differs")
    write_json(
        out / "chi-source.json",
        {
            "upstream": "https://github.com/go-chi/chi.git",
            "revision": CHI_REVISION,
            "sum": CHI_SUM,
            "files": {name: digest(rooted(source, name)) for name in files},
        },
    )
    return source, files


def test_checks(
    go: Path, repo: Path, out: Path, env: dict[str, str]
) -> list[dict[str, object]]:
    overlay = out / "old-production.json"
    write_json(
        overlay,
        {
            "Replace": {
                str(repo / "internal/analyzers/chanblock/chanblock.go"): str(
                    repo / DIRECTORY / "old/chanblock.go.txt"
                )
            }
        },
    )
    common = (
        str(go),
        "test",
        "-count=1",
        "-timeout=10m",
        "-p=1",
        "-parallel=1",
        "-json",
    )
    checks = (
        (
            "old-production",
            ANALYZER,
            frozenset((ROOT, NEGATIVE)),
            True,
            (
                f"-overlay={overlay}",
                "./internal/analyzers/chanblock",
                "-run",
                f"^{ROOT}$/^communications$",
            ),
        ),
        (
            "positive",
            ANALYZER,
            ANALYZER_NAMES,
            False,
            ("./internal/analyzers/chanblock",),
        ),
        (
            "race",
            ANALYZER,
            ANALYZER_NAMES,
            False,
            ("-race", "./internal/analyzers/chanblock"),
        ),
        (
            "parity",
            RULES,
            PARITY_NAMES,
            False,
            (
                "./internal/rules",
                "-run",
                "^TestLogosPorts$/^correctness_unbounded_channel_op$",
            ),
        ),
    )
    results: list[dict[str, object]] = []
    for label, package, names, negative, arguments in checks:
        run = capture(common + arguments, repo, env, out, label)
        record: dict[str, object] = {
            "label": label,
            "run": asdict(run),
            "accepted": False,
        }
        try:
            stdout, stderr, code = streams(run)
            check_test_output(stdout, stderr, code, package, names, negative=negative)
            record["accepted"] = True
        except (OSError, TypeError, UnicodeError, ValueError) as exc:
            record["error"] = str(exc)
        results.append(record)
        write_json(out / f"{label}.check.json", record)
    return results


def cli_checks(
    go: Path, repo: Path, out: Path, env: dict[str, str]
) -> list[dict[str, object]]:
    binary = out / "gglint"
    require_success(
        capture(
            (str(go), "build", "-trimpath", "-o", str(binary), "./cmd/gglint"),
            repo,
            env,
            out,
            "build",
        ),
        quiet=True,
    )
    binary_hash = digest(binary)
    require_success(
        capture(
            (str(go), "version", "-m", str(binary)), repo, env, out, "binary-buildinfo"
        )
    )
    source, files = prepare_chi(out, env)
    repeat = out / "chi-repeat"
    repeat.mkdir()
    for name in files:
        target = repeat / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(rooted(source, name), target)
    config = out / "chi.toml"
    shutil.copyfile(repo / DIRECTORY / "evidence/chi.toml", config)
    config_relative = "../chi.toml"
    cli_env = env | {"CGO_ENABLED": "0"}
    write_json(
        out / "environment-cli.json",
        {key: cli_env.get(key) for key in ENVIRONMENT_KEYS},
    )
    records: list[dict[str, object]] = []
    for mode in ("basic", "standard", "strict"):
        expected = (
            decode((repo / DIRECTORY / "expected-chi-strict-findings.json").read_text())
            if mode == "strict"
            else mapping(
                decode(
                    (repo / DIRECTORY / f"evidence/gglint-{mode}.stdout").read_text()
                )
            )["findings"]
        )
        for label, work in (("primary", source), ("repeat", repeat)):
            if tree_sum(work, files) != CHI_SUM or digest(binary) != binary_hash:
                raise ValueError("CLI source or tool changed before replay")
            run = capture(
                (
                    str(binary),
                    "run",
                    "-config",
                    str(config),
                    "-mode",
                    mode,
                    "-format",
                    "json",
                    "./...",
                ),
                work,
                cli_env,
                out,
                f"chi-{mode}-{label}",
                timeout=300,
            )
            record: dict[str, object] = {
                "mode": mode,
                "copy": label,
                "run": asdict(run),
                "accepted": False,
            }
            try:
                stdout, stderr, code = streams(run)
                check_cli_output(stdout, stderr, code, mode, config_relative, expected)
                record["accepted"] = True
            except (OSError, TypeError, UnicodeError, ValueError) as exc:
                record["error"] = str(exc)
            records.append(record)
            write_json(out / f"chi-{mode}-{label}.check.json", record)
    if (
        any(tree_sum(work, files) != CHI_SUM for work in (source, repeat))
        or digest(binary) != binary_hash
    ):
        raise ValueError("CLI source or tool changed after replay")
    if digest(config) != digest(repo / DIRECTORY / "evidence/chi.toml"):
        raise ValueError("CLI configuration changed")
    write_json(
        out / "binary.json",
        {
            "path": str(binary),
            "sha256_before": binary_hash,
            "sha256_after": digest(binary),
        },
    )
    return records


def run(repo: Path, out: Path, expected_head: str) -> int:
    out.mkdir(parents=True, exist_ok=False)
    report: dict[str, object] = {
        "schema_version": 1,
        "accepted": False,
        "expected_head": expected_head,
    }
    before_source: dict[str, object] | None = None
    before_tools: dict[str, dict[str, str]] | None = None
    go: Path | None = None
    failed = False
    try:
        report["frozen"] = verify_frozen(repo)
        before_source = source_identity(repo, expected_head)
        found = shutil.which("go")
        if found is None:
            raise ValueError("Go is missing from PATH")
        go = Path(found).resolve(strict=True)
        before_tools = tool_identity(go)
        write_json(out / "source-before.json", before_source)
        write_json(out / "tools-before.json", before_tools)
        env = os.environ.copy()
        for key in ("GOROOT", "GOEXPERIMENT", "GODEBUG"):
            env.pop(key, None)
        env.update(
            {
                "GOFLAGS": "-mod=readonly -buildvcs=false -p=1",
                "GOENV": "off",
                "GOWORK": "off",
                "GOTOOLCHAIN": "local",
                "GOPROXY": "off",
                "GOSUMDB": "off",
                "GOTELEMETRY": "off",
                "GOMAXPROCS": "2",
                "GOOS": "linux",
                "GOARCH": "amd64",
                "GOAMD64": "v1",
                "CGO_ENABLED": "1",
                "PATH": str(go.parent) + os.pathsep + env.get("PATH", ""),
            }
        )
        write_json(
            out / "environment.json", {key: env.get(key) for key in ENVIRONMENT_KEYS}
        )
        version = capture(
            (str(go), "version"), repo, env, out, "go-version", timeout=30
        )
        stdout, stderr, code = streams(version)
        if code != 0 or stderr or stdout != GO_VERSION:
            raise ValueError("the gate requires stock Go 1.27.4 linux/amd64")
        tests = test_checks(go, repo, out, env)
        report["tests"] = tests
        cli = cli_checks(go, repo, out, env)
        report["cli"] = cli
        failed = any(record["accepted"] is not True for record in tests + cli)
    except (
        OSError,
        TypeError,
        UnicodeError,
        ValueError,
        subprocess.SubprocessError,
    ) as exc:
        report["error"] = str(exc)
        failed = True
    finally:
        try:
            report["frozen_after"] = verify_frozen(repo)
            after_source = source_identity(repo, expected_head)
            write_json(out / "source-after.json", after_source)
            if before_source != after_source or go is None:
                raise ValueError("source identity changed or tool was unavailable")
            after_tools = tool_identity(go)
            write_json(out / "tools-after.json", after_tools)
            if before_tools != after_tools:
                raise ValueError("tool identity changed")
        except (
            OSError,
            TypeError,
            UnicodeError,
            ValueError,
            subprocess.SubprocessError,
        ) as exc:
            report["identity_error"] = str(exc)
            failed = True
        report["accepted"] = not failed
        write_json(out / "report.json", report)
    return int(failed)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", required=True, type=Path)
    parser.add_argument("--evidence", required=True, type=Path)
    parser.add_argument("--expected-head", required=True)
    args = parser.parse_args()
    repo = cast(Path, args.repo).resolve(strict=True)
    out = cast(Path, args.evidence).resolve()
    head = cast(str, args.expected_head)
    if re.fullmatch(r"[0-9a-f]{40}", head) is None or out.is_relative_to(repo):
        parser.error(
            "expected-head must be a full commit; evidence must be outside the checkout"
        )
    return run(repo, out, head)


if __name__ == "__main__":
    raise SystemExit(main())
