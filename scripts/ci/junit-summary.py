#!/usr/bin/env python3
"""Print failing tests from JUnit XML files as Markdown and GitHub annotations.

Reads the globs in JUNIT_GLOBS (space separated, relative to the repository
root; defaults cover gotestsum, nextest, Vitest and Playwright). Markdown goes
to stdout; `::error file=..,line=..::` lines go to stdout too, at most ten.
Exits 0 always: the job status comes from the test step, not from this script.
"""
import glob
import os
import re
import sys
import xml.etree.ElementTree as ET

DEFAULT_GLOBS = [
    os.path.join(os.environ.get("RUNNER_TEMP", "/nonexistent"), "junit", "*.xml"),
    "engine/target/nextest/*/junit.xml",
    "apps/client/test-results/*junit*.xml",
]
LOCATION = re.compile(r"((?:[\w@.-]+/)*[\w@.-]+\.(?:go|rs|tsx?|jsx?|mjs)):(\d+)")
SKIP_PATH = ("node_modules/", "/.cargo/", "/rustc/", "/usr/")
MAX_LISTED = 50
MAX_ANNOTATIONS = 10


def go_module():
    try:
        with open("services/api/go.mod", encoding="utf-8") as handle:
            return handle.readline().split()[1]
    except (OSError, IndexError):
        return ""


def repo_path(path, classname, module):
    workspace = os.environ.get("GITHUB_WORKSPACE", os.getcwd()).rstrip("/") + "/"
    if path.startswith(workspace):
        path = path[len(workspace):]
    if os.path.exists(path):
        return path
    if module and classname.startswith(module):
        candidate = os.path.join("services/api", classname[len(module):].lstrip("/"), path)
        if os.path.exists(candidate):
            return candidate
    return path


def locate(text, classname, module):
    for match in LOCATION.finditer(text):
        if any(skip in match.group(1) for skip in SKIP_PATH):
            continue
        return repo_path(match.group(1), classname, module), int(match.group(2))
    return None, None


def escape(value):
    return value.replace("%", "%25").replace("\r", "%0D").replace("\n", "%0A")


def main():
    patterns = os.environ.get("JUNIT_GLOBS", "").split() or DEFAULT_GLOBS
    files = sorted({path for pattern in patterns for path in glob.glob(pattern)})
    if not files:
        return
    module = go_module()
    total = failed = skipped = 0
    failures = []
    for path in files:
        try:
            root = ET.parse(path).getroot()
        except (ET.ParseError, OSError) as error:
            print(f"- Could not read `{path}`: {error}")
            continue
        for case in root.iter("testcase"):
            total += 1
            if case.find("skipped") is not None:
                skipped += 1
            bad = case.find("failure")
            if bad is None:
                bad = case.find("error")
            if bad is None:
                continue
            failed += 1
            text = (bad.get("message") or "") + "\n" + (bad.text or "")
            classname = case.get("classname") or ""
            file, line = locate(text, classname, module)
            failures.append((classname, case.get("name") or "?", file, line, text.strip()))

    print(f"- Tests: {total} run, **{failed} failed**, {skipped} skipped")
    if not failures:
        return
    print("\n#### Failing tests\n")
    for classname, name, file, line, text in failures[:MAX_LISTED]:
        where = f" `{file}:{line}`" if file else ""
        print(f"- `{classname} {name}`{where}")
        excerpt = " ".join(text.split())[:300]
        if excerpt:
            print(f"  > {excerpt}")
    if len(failures) > MAX_LISTED:
        print(f"- ... and {len(failures) - MAX_LISTED} more")
    for classname, name, file, line, text in failures[:MAX_ANNOTATIONS]:
        rows = [row.strip() for row in text.splitlines() if row.strip()]
        first = next((row for row in rows if LOCATION.search(row)), rows[0] if rows else "failed")
        location = f" file={file},line={line}," if file else " "
        print(f"::error{location}title={escape(name)}::{escape(first[:300])}")


if __name__ == "__main__":
    sys.exit(main())
