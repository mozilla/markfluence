# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Smoke tests: prove the CLI is wired up and importable."""

from click.testing import CliRunner

from mzcld_confluence_cli.cli import main


def test_group_help():
    result = CliRunner().invoke(main, ["--help"])
    assert result.exit_code == 0
    assert "update" in result.output
    assert "create" in result.output


def test_update_help():
    result = CliRunner().invoke(main, ["update", "--help"])
    assert result.exit_code == 0
    assert "FILENAMES" in result.output


def test_create_help():
    result = CliRunner().invoke(main, ["create", "--help"])
    assert result.exit_code == 0
    assert "FILENAMES" in result.output
    assert "--space" in result.output
    assert "--parent" in result.output
