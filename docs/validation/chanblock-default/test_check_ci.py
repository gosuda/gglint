# pyright: strict
"""Fail-closed controls for hosted evidence, without executing Go."""

from __future__ import annotations

import copy
import hashlib
import json
import os
import shutil
import sys
import tarfile
from pathlib import Path

import check_ci as check
import pytest
from hypothesis import given
from hypothesis import strategies as st

Event = dict[str, object]


@pytest.fixture(scope="module")
def hosted_evidence() -> dict[str, bytes]:
    directory = Path(__file__).parent / "output-type"
    manifest_path = directory / "frozen.json"
    assert check.digest(manifest_path) == (
        "02e79608d2913039bab552b8084afaed593f873378d3190f7269aaa5eb9bb061"
    )
    manifest = check.mapping(check.decode(manifest_path.read_text()))
    archive = directory / "failed-hosted-evidence.tar.gz"
    assert check.digest(archive) == (
        "1d983d8eb9a6e990159cb6546f8066aef7f90e088ea84071e8d08d8d6c64bd97"
    )
    files = check.mapping(manifest["files"])
    result: dict[str, bytes] = {}
    with tarfile.open(archive, "r:gz") as source:
        for entry in source:
            assert entry.isfile() and entry.name not in result
            stream = source.extractfile(entry)
            assert stream is not None
            with stream:
                data = stream.read()
            expected = check.mapping(files[entry.name])
            assert hashlib.sha256(data).hexdigest() == expected["sha256"]
            assert len(data) == expected["bytes"]
            result[entry.name] = data
    assert set(result) == set(files)
    return result


def hosted_stream(evidence: dict[str, bytes], label: str) -> tuple[str, bytes, int]:
    prefix = f"artifact/chanblock-default/{label}"
    receipt = check.mapping(check.decode(evidence[f"{prefix}.run.json"].decode()))
    stdout, stderr = evidence[f"{prefix}.stdout"], evidence[f"{prefix}.stderr"]
    assert receipt["timed_out"] is False and receipt["error"] == ""
    assert hashlib.sha256(stdout).hexdigest() == receipt["stdout_sha256"]
    assert hashlib.sha256(stderr).hexdigest() == receipt["stderr_sha256"]
    code = receipt["returncode"]
    assert type(code) is int
    return stdout.decode(), stderr, code


@pytest.mark.parametrize(
    "label,package,names,code",
    (
        (
            "old-production",
            check.ANALYZER,
            frozenset((check.ROOT, check.NEGATIVE)),
            1,
        ),
        ("positive", check.ANALYZER, check.ANALYZER_NAMES, 0),
        ("race", check.ANALYZER, check.ANALYZER_NAMES, 0),
        ("parity", check.RULES, check.PARITY_NAMES, 0),
    ),
)
def test_observed_go1274_test_streams(
    hosted_evidence: dict[str, bytes],
    label: str,
    package: str,
    names: frozenset[str],
    code: int,
) -> None:
    stdout, stderr, recorded_code = hosted_stream(hosted_evidence, label)
    assert recorded_code == code
    check.check_test_output(
        stdout,
        stderr,
        code,
        package,
        names,
        negative=label == "old-production",
    )


@pytest.mark.parametrize("mode", ("basic", "standard", "strict"))
@pytest.mark.parametrize("copy_name", ("primary", "repeat"))
def test_observed_six_chi_comparisons(
    hosted_evidence: dict[str, bytes], mode: str, copy_name: str
) -> None:
    stdout, stderr, code = hosted_stream(hosted_evidence, f"chi-{mode}-{copy_name}")
    directory = Path(__file__).parent
    expected = (
        check.decode((directory / "expected-chi-strict-findings.json").read_text())
        if mode == "strict"
        else check.mapping(
            check.decode((directory / f"evidence/gglint-{mode}.stdout").read_text())
        )["findings"]
    )
    check.check_cli_output(stdout, stderr, code, mode, "../chi.toml", expected)


def event(
    action: str,
    name: str = "",
    package: str = check.ANALYZER,
    output: str | None = None,
) -> Event:
    result: Event = {"Action": action, "Package": package}
    if name:
        result["Test"] = name
    if output is not None:
        result["Output"] = output
    return result


def encode(events: list[Event]) -> str:
    return "".join(json.dumps(item) + "\n" for item in events)


def positive(
    names: frozenset[str] = check.ANALYZER_NAMES, package: str = check.ANALYZER
) -> list[Event]:
    events = [event("start", package=package)]
    for root in sorted(name for name in names if "/" not in name):
        events.extend(
            (
                event("run", root, package),
                event("pause", root, package),
                event("cont", root, package),
            )
        )
        for child in sorted(name for name in names if name.startswith(root + "/")):
            events.extend(
                (
                    event("run", child, package),
                    event("pause", child, package),
                    event("cont", child, package),
                    event("pass", child, package),
                )
            )
        events.append(event("pass", root, package))
    events.append(event("pass", package=package))
    return events


def negative() -> list[Event]:
    events = [event("start"), event("run", check.ROOT), event("run", check.NEGATIVE)]
    events.extend(
        event(
            "output", check.NEGATIVE, output=f"    analysistest.go:651: {diagnostic}\n"
        )
        for diagnostic in check.expected_diagnostics()
    )
    events.extend(
        (
            event(
                "output",
                check.NEGATIVE,
                output=f"    --- FAIL: {check.NEGATIVE} (0.01s)\n",
            ),
            event("fail", check.NEGATIVE),
            event("fail", check.ROOT),
            event("output", output="FAIL\n"),
            event("fail"),
        )
    )
    return events


def accept(
    events: list[Event],
    *,
    old: bool = False,
    code: int | None = None,
    stderr: bytes = b"",
) -> None:
    names = frozenset((check.ROOT, check.NEGATIVE)) if old else check.ANALYZER_NAMES
    check.check_test_output(
        encode(events),
        stderr,
        (1 if old else 0) if code is None else code,
        check.ANALYZER,
        names,
        negative=old,
    )


def test_exact_positive_and_negative() -> None:
    accept(positive())
    accept(negative(), old=True)
    check.check_test_output(
        encode(positive(check.PARITY_NAMES, check.RULES)),
        b"",
        0,
        check.RULES,
        check.PARITY_NAMES,
    )


@pytest.mark.parametrize("output_type", ("", "frame", "error", "error-continue"))
def test_documented_output_types(output_type: str) -> None:
    events = negative()
    events[3]["OutputType"] = output_type
    accept(events, old=True)


@pytest.mark.parametrize(
    "value", (None, False, 1, 0.0, [], {}, "unknown", "Frame", "error\n")
)
def test_invalid_output_type(value: object) -> None:
    events = negative()
    events[3]["OutputType"] = value
    with pytest.raises((TypeError, ValueError)):
        accept(events, old=True)


@pytest.mark.parametrize("action", ("start", "run", "pause", "cont", "pass"))
@pytest.mark.parametrize("output_type", ("", "frame", "error", "error-continue"))
def test_output_type_requires_output_action(action: str, output_type: str) -> None:
    events = positive()
    next(item for item in events if item["Action"] == action)["OutputType"] = (
        output_type
    )
    with pytest.raises(ValueError):
        accept(events)


def test_output_type_requires_output_text() -> None:
    events = negative()
    events[3]["OutputType"] = "error"
    del events[3]["Output"]
    with pytest.raises((TypeError, ValueError)):
        accept(events, old=True)


def test_output_type_does_not_admit_unknown_fields() -> None:
    events = negative()
    events[3].update({"OutputType": "error", "Unknown": "ignored data"})
    with pytest.raises(ValueError, match="unknown test event field"):
        accept(events, old=True)


@pytest.mark.parametrize("output_type", ("", "frame", "error", "error-continue"))
def test_output_type_does_not_hide_crash(output_type: str) -> None:
    events = positive()
    crash = event("output", "TestAnalyzer", output="panic: broken\n")
    crash["OutputType"] = output_type
    events.insert(4, crash)
    with pytest.raises(ValueError):
        accept(events)


@pytest.mark.parametrize("name", sorted(check.ANALYZER_NAMES))
def test_missing_or_skipped_named_result(name: str) -> None:
    missing = [
        item
        for item in positive()
        if not (item.get("Test") == name and item["Action"] == "pass")
    ]
    with pytest.raises(ValueError):
        accept(missing)
    skipped = positive()
    next(
        item
        for item in skipped
        if item.get("Test") == name and item["Action"] == "pass"
    )["Action"] = "skip"
    with pytest.raises(ValueError):
        accept(skipped)


@pytest.mark.parametrize("action", ("run", "pass", "pause", "cont"))
def test_duplicate_test_event(action: str) -> None:
    events = positive()
    index = next(
        i
        for i, item in enumerate(events)
        if item["Action"] == action and item.get("Test") == "TestAnalyzer"
    )
    events.insert(index, copy.copy(events[index]))
    with pytest.raises(ValueError):
        accept(events)


@pytest.mark.parametrize(
    "text",
    (
        "panic: broken\n",
        "fatal error: bad map\n",
        "compiler: signal: killed\n",
        "SIGSEGV\n",
        "test timed out after 1m\n",
    ),
)
@pytest.mark.parametrize("owner", ("", "TestAnalyzer"))
def test_crash_output_rejected(text: str, owner: str) -> None:
    events = positive()
    events.insert(4, event("output", owner, output=text))
    with pytest.raises(ValueError):
        accept(events)


@pytest.mark.parametrize("action", ("build-output", "build-fail"))
def test_build_events_rejected(action: str) -> None:
    events = positive()
    events.insert(
        0, {"Action": action, "ImportPath": check.ANALYZER, "Output": "build failed\n"}
    )
    with pytest.raises(ValueError, match="build event"):
        accept(events)


@pytest.mark.parametrize("placement", ("before-package", "after-package", "after-test"))
def test_output_lifetime(placement: str) -> None:
    events = positive()
    if placement == "before-package":
        events.insert(0, event("output", output="noise\n"))
    elif placement == "after-package":
        events.append(event("output", output="noise\n"))
    else:
        index = next(
            i
            for i, item in enumerate(events)
            if item.get("Test") == "TestAnalyzer" and item["Action"] == "pass"
        )
        events.insert(index + 1, event("output", "TestAnalyzer", output="noise\n"))
    with pytest.raises(ValueError):
        accept(events)


@pytest.mark.parametrize(
    "field,value",
    (("Package", "wrong/package"), ("Action", "mystery"), ("Test", "WrongTest")),
)
def test_unknown_event_identity(field: str, value: str) -> None:
    events = positive()
    events[1][field] = value
    with pytest.raises(ValueError):
        accept(events)


@pytest.mark.parametrize(
    "change", ("empty-name", "unknown-field", "misplaced-output", "non-object")
)
def test_malformed_event_schema(change: str) -> None:
    events = positive()
    if change == "empty-name":
        events[0]["Test"] = ""
    elif change == "unknown-field":
        events[0]["Unknown"] = "ignored data"
    elif change == "misplaced-output":
        events[-1]["Output"] = "panic: misplaced\n"
    else:
        with pytest.raises(TypeError):
            check.check_test_output(
                "[]\n", b"", 0, check.ANALYZER, check.ANALYZER_NAMES
            )
        return
    with pytest.raises(ValueError):
        accept(events)


@pytest.mark.parametrize("old", (False, True))
def test_status_and_stderr_required(old: bool) -> None:
    events = negative() if old else positive()
    with pytest.raises(ValueError):
        accept(events, old=old, code=0 if old else 1)
    with pytest.raises(ValueError):
        accept(events, old=old, stderr=b"unexpected stderr\n")


@given(st.sampled_from(tuple(check.expected_diagnostics())))
def test_every_negative_diagnostic_is_required_once(diagnostic: str) -> None:
    events = negative()
    index = next(
        i for i, item in enumerate(events) if diagnostic in str(item.get("Output", ""))
    )
    removed = events[:index] + events[index + 1 :]
    with pytest.raises(ValueError, match="exact eight"):
        accept(removed, old=True)
    duplicated = events[:index] + [copy.copy(events[index])] + events[index:]
    with pytest.raises(ValueError, match="exact eight"):
        accept(duplicated, old=True)


@pytest.mark.parametrize("change", ("owner", "location", "message", "extra-failure"))
def test_negative_cause_is_exact(change: str) -> None:
    events = negative()
    output = next(
        item
        for item in events
        if "unexpected diagnostic" in str(item.get("Output", ""))
    )
    if change == "owner":
        output["Test"] = check.ROOT
    elif change == "location":
        output["Output"] = str(output["Output"]).replace(":5:7:", ":6:7:")
    elif change == "message":
        output["Output"] = str(output["Output"]).replace(
            "can block forever", "some other error"
        )
    else:
        events.insert(
            3,
            event(
                "output",
                check.NEGATIVE,
                output="    test.go:1: loading package failed\n",
            ),
        )
    with pytest.raises(ValueError):
        accept(events, old=True)


@pytest.mark.parametrize(
    "text",
    (
        '{"Action":"start","Action":"pass"}',
        '{"value":NaN}',
        '{"value":Infinity}',
        "not json",
    ),
)
def test_invalid_json(text: str) -> None:
    with pytest.raises(ValueError):
        check.decode(text)


def cli(findings: object) -> str:
    return json.dumps({"mode": "strict", "config": "../chi.toml", "findings": findings})


def test_cli_exact_array_and_invocation() -> None:
    expected: object = [
        {"rule": "chanblock", "path": "a.go", "line": 1, "message": "bounded fixture"}
    ]
    stderr = b"gglint: config ../chi.toml\n"
    check.check_cli_output(cli(expected), stderr, 1, "strict", "../chi.toml", expected)
    for actual in ([], [expected, expected], [{"rule": "different"}], True, None):
        with pytest.raises(ValueError):
            check.check_cli_output(
                cli(actual), stderr, 1, "strict", "../chi.toml", expected
            )
    with pytest.raises(ValueError):
        check.check_cli_output(
            cli(expected), stderr, 0, "strict", "../chi.toml", expected
        )
    with pytest.raises(ValueError):
        check.check_cli_output(
            cli(expected), stderr, 1, "standard", "../chi.toml", expected
        )
    with pytest.raises(ValueError):
        check.check_cli_output(
            cli(expected), stderr, 1, "strict", "../other.toml", expected
        )
    with pytest.raises(ValueError):
        check.check_cli_output(
            cli(expected),
            stderr + b"panic: hidden\n",
            1,
            "strict",
            "../chi.toml",
            expected,
        )


@pytest.mark.parametrize("replacement", (True, 1.0))
def test_cli_json_types_are_exact(replacement: object) -> None:
    expected: object = [{"line": 1}]
    with pytest.raises(ValueError):
        check.check_cli_output(
            cli([{"line": replacement}]),
            b"gglint: config ../chi.toml\n",
            1,
            "strict",
            "../chi.toml",
            expected,
        )


@pytest.mark.parametrize("path", ("../outside", "/etc/passwd", "", "bad\nname"))
def test_paths_are_contained(tmp_path: Path, path: str) -> None:
    with pytest.raises(ValueError):
        check.rooted(tmp_path, path)


def test_symlink_is_rejected(tmp_path: Path) -> None:
    target = tmp_path / "real"
    target.write_text("safe")
    (tmp_path / "link").symlink_to(target)
    with pytest.raises(ValueError):
        check.rooted(tmp_path, "link")


def test_source_digest_tracks_content_and_inventory(tmp_path: Path) -> None:
    source = tmp_path / "source.go"
    source.write_text("package first\n")
    files = ("source.go",)
    before = check.tree_sum(tmp_path, files)
    source.write_text("package second\n")
    assert check.tree_sum(tmp_path, files) != before
    (tmp_path / "extra.go").write_text("package second\n")
    with pytest.raises(ValueError, match="inventory"):
        check.tree_sum(tmp_path, files)


def test_tool_identity_tracks_executable_bytes(tmp_path: Path) -> None:
    go = tmp_path / "bin/go"
    go.parent.mkdir()
    go.write_bytes(b"inert go; never executed")
    tools = tmp_path / "pkg/tool/linux_amd64"
    tools.mkdir(parents=True)
    for name in ("compile", "link", "asm", "vet"):
        (tools / name).write_bytes(b"inert tool; never executed")
    before = check.tool_identity(go)
    (tools / "compile").write_bytes(b"changed compiler")
    after = check.tool_identity(go)
    assert before["compile"] != after["compile"]
    assert before["go"] == after["go"]


def test_source_identity_rejects_revision_and_worktree_drift(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    expected = "a" * 40
    source = tmp_path / "source.go"
    source.write_text("package first\n")
    state = {"head": expected, "status": ""}

    def git_value(_repo: Path, *args: str) -> str:
        if args == ("rev-parse", "HEAD"):
            return state["head"]
        if args == ("rev-parse", "HEAD^{tree}"):
            return "b" * 40
        assert args == ("status", "--porcelain=v1")
        return state["status"]

    def tracked_files(_root: Path) -> tuple[str, ...]:
        return ("source.go",)

    monkeypatch.setattr(check, "git_value", git_value)
    monkeypatch.setattr(check, "tracked_files", tracked_files)
    before = check.source_identity(tmp_path, expected)
    source.write_text("package second\n")
    assert check.source_identity(tmp_path, expected) != before
    state["status"] = " M source.go"
    with pytest.raises(ValueError):
        check.source_identity(tmp_path, expected)
    state.update(head="c" * 40, status="")
    with pytest.raises(ValueError):
        check.source_identity(tmp_path, expected)


def test_frozen_manifest_and_tampering(tmp_path: Path) -> None:
    repo = Path(__file__).resolve().parents[3]
    frozen = check.verify_frozen(repo)
    assert len(frozen) == 32
    for relative in (*frozen, f"{check.DIRECTORY}/FROZEN.sha256"):
        destination = tmp_path / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(repo / relative, destination)
    check.verify_frozen(tmp_path)
    (tmp_path / check.DIRECTORY / "expected-chi-strict-findings.json").write_text(
        "[]\n"
    )
    with pytest.raises(ValueError, match="frozen input differs"):
        check.verify_frozen(tmp_path)


def test_capture_and_log_tampering(tmp_path: Path) -> None:
    result = check.capture(
        (sys.executable, "-c", "print('receipt')"),
        tmp_path,
        os.environ.copy(),
        tmp_path,
        "python",
        timeout=10,
    )
    assert check.streams(result) == ("receipt\n", b"", 0)
    Path(result.stdout).write_text("changed\n")
    with pytest.raises(ValueError, match="output changed"):
        check.streams(result)


def test_timeout_is_not_a_negative_result(tmp_path: Path) -> None:
    result = check.capture(
        (sys.executable, "-c", "import time; time.sleep(30)"),
        tmp_path,
        os.environ.copy(),
        tmp_path,
        "timeout",
        timeout=1,
    )
    assert result.timed_out
    with pytest.raises(ValueError, match="complete normally"):
        check.streams(result)
