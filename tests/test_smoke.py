"""Smoke tests: prove the CLI is wired up and importable."""

from click.testing import CliRunner

from mzcld_confluence_cli.cli import main


def test_group_help():
    result = CliRunner().invoke(main, ["--help"])
    assert result.exit_code == 0
    assert "update" in result.output


def test_update_help():
    result = CliRunner().invoke(main, ["update", "--help"])
    assert result.exit_code == 0
    assert "FILENAMES" in result.output
