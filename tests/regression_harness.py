# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Shared harness for the md_to_confluence golden-file regression suite.

Both the standalone golden generator (``generate_regression_goldens.py``) and the
pytest comparer (``test_regression.py``) import from here so that the
run/redact/normalize logic stays identical between generating and checking.

A case is a directory under ``tests/regression/``. It holds real fixture files
exactly as :func:`markfluence.libmarkdown.md_to_confluence` consumes them:

* a primary markdown file (``main.md`` by default) whose frontmatter is stripped
  before conversion, just like the ``update``/``create`` commands do;
* optional sibling ``*.md`` files (their frontmatter carries the ``page_id`` /
  ``title`` / headings that link- and anchor-rewriting key off);
* optional image files under ``assets/`` (only existence + extension matter);
* an optional ``test.input`` (JSON) carrying scalar params and a fixture-file
  manifest -- omitted entirely when the case takes all defaults and has no
  fixtures beyond ``main.md``;
* a generated ``test.output`` golden (pretty-printed, sorted keys).

The suite runs against the checked-in directories in place (conversion never
writes), and redacts the case-directory prefix from attachment paths so goldens
are stable across machines.
"""

import json
import os
from dataclasses import asdict

from markfluence.libmarkdown import MarkdownFile, md_to_confluence

REGRESSION_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "regression")

# Files that belong to the harness, not the fixture set it verifies.
RESERVED = {"test.input", "test.output"}

# Attachment paths are absolute (os.path.abspath); the case-dir prefix is
# replaced with this token so the golden is machine-independent.
ROOT_TOKEN = "<ROOT>"

DEFAULTS = {
    "filename": "main.md",
    "base_url": "https://wiki.example.net",
    "space_key": "ENG",
}


def list_cases():
    """Return the sorted absolute paths of the case directories."""
    cases = []
    for name in sorted(os.listdir(REGRESSION_DIR)):
        if name.startswith((".", "_")):
            continue
        path = os.path.join(REGRESSION_DIR, name)
        if os.path.isdir(path):
            cases.append(path)
    return cases


def _fixture_files(case_dir):
    """Return the set of fixture files in ``case_dir`` (relative, ``/``-joined).

    Excludes the reserved ``test.input`` / ``test.output`` and any dotted or
    dunder directories (e.g. ``__pycache__``).
    """
    found = set()
    for dirpath, dirnames, filenames in os.walk(case_dir):
        dirnames[:] = [d for d in dirnames if not d.startswith((".", "__"))]
        for filename in filenames:
            rel = os.path.relpath(os.path.join(dirpath, filename), case_dir)
            rel = rel.replace(os.sep, "/")
            if rel in RESERVED:
                continue
            found.add(rel)
    return found


def load_case(case_dir):
    """Read ``test.input`` (if any), apply defaults, and enforce the manifest.

    Returns the resolved config dict (``filename``, ``base_url``, ``space_key``,
    ``files``). Raises ``AssertionError`` if the directory's fixture files don't
    exactly match the declared ``files`` manifest, or if the primary ``filename``
    isn't listed in it.
    """
    config: dict = dict(DEFAULTS)
    input_path = os.path.join(case_dir, "test.input")
    if os.path.isfile(input_path):
        with open(input_path) as f:
            config.update(json.load(f))
    config.setdefault("files", [config["filename"]])

    case = os.path.basename(case_dir)
    actual = _fixture_files(case_dir)
    declared = set(config["files"])
    if actual != declared:
        raise AssertionError(
            f"regression case {case!r}: fixture files don't match test.input.\n"
            f"  missing (declared, absent on disk): {sorted(declared - actual)}\n"
            f"  stray (on disk, not declared):     {sorted(actual - declared)}"
        )
    if config["filename"] not in declared:
        raise AssertionError(
            f"regression case {case!r}: primary filename {config['filename']!r} "
            f"is not listed in test.input 'files'"
        )
    return config


def _normalize(page, case_dir):
    """Turn a ConfluencePage into a golden-comparable dict.

    Redacts the case-directory prefix from each attachment path so the result
    doesn't depend on where the repository lives.
    """
    data = asdict(page)
    root = os.path.abspath(case_dir)
    for attachment in data["attachments"]:
        abspath = os.path.abspath(attachment.get("path", ""))
        if abspath == root or abspath.startswith(root + os.sep):
            rel = abspath[len(root) :].replace(os.sep, "/")
            attachment["path"] = ROOT_TOKEN + rel
    return data


def run_case(case_dir):
    """Run one case through md_to_confluence and return the normalized dict."""
    config = load_case(case_dir)
    primary = os.path.join(case_dir, config["filename"])
    mdfile = MarkdownFile.from_path(primary)
    page = md_to_confluence(mdfile, config["base_url"], config["space_key"])
    return _normalize(page, case_dir)


def dumps(data):
    """Serialize a normalized dict to the golden's on-disk form."""
    return json.dumps(data, indent=2, sort_keys=True) + "\n"


def read_golden(case_dir):
    """Read and parse the stored ``test.output`` golden for a case."""
    with open(os.path.join(case_dir, "test.output")) as f:
        return json.load(f)
