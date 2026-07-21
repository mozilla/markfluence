# List available recipes
default:
    @just --list

# Run linting, tests, and typechecking
check: lint test typecheck

# Lint and check formatting with ruff
lint:
    uv run ruff check
    uv run ruff format --check

# Run the test suite
test:
    uv run pytest

# Typecheck with ty
typecheck:
    uv run ty check

# Regenerate the md_to_confluence regression goldens (review the diff before committing)
regen-regressions:
    uv run python tests/generate_regression_goldens.py
