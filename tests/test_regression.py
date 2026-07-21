# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Golden-file regression tests for md_to_confluence.

Each case under ``tests/regression/`` is a directory of real fixture files plus a
generated ``test.output`` golden. See ``regression_harness.py`` for the layout.
Regenerate goldens after an intentional change with ``just regen-regressions``,
then review the diff.
"""

import os

import pytest

from regression_harness import list_cases, read_golden, run_case

CASES = list_cases()


@pytest.mark.parametrize("case_dir", CASES, ids=[os.path.basename(c) for c in CASES])
def test_regression(case_dir):
    case = os.path.basename(case_dir)
    assert os.path.isfile(os.path.join(case_dir, "test.output")), (
        f"missing golden for {case!r}; run `just regen-regressions` to create it"
    )
    actual = run_case(case_dir)
    expected = read_golden(case_dir)
    assert actual == expected, (
        f"regression mismatch for {case!r}; review the change and, if intended, "
        f"run `just regen-regressions` to update the golden"
    )
