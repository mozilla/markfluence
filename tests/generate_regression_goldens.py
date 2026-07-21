# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""(Re)generate the ``test.output`` goldens for the regression suite.

Run via ``just regen-regressions``. Walks every case under ``tests/regression/``,
runs it through the shared harness, and (over)writes each ``test.output``. Review
the resulting diff before committing -- an unexpected change here is the whole
point of the suite.
"""

import os

from regression_harness import dumps, list_cases, run_case


def main():
    for case_dir in list_cases():
        data = run_case(case_dir)
        out_path = os.path.join(case_dir, "test.output")
        with open(out_path, "w") as f:
            f.write(dumps(data))
        print(f"wrote {os.path.relpath(out_path)}")


if __name__ == "__main__":
    main()
