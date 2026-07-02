"""HTTP client for the Confluence REST API.

Wraps an ``httpx2.Client`` with basic auth and the handful of API calls the
CLI needs. Configuration is read from the environment (see
:meth:`ConfluenceClient.from_env`).

Request URLs are built as absolute URLs off ``base_url`` (rather than relying on
``httpx2``'s relative-URL joining against a ``base_url`` path), matching how the
original ``confluence_publish.py`` script constructed them.
"""

import os

import click
import httpx2


class ConfluenceClient:
    """Thin wrapper over the Confluence v2 REST API."""

    def __init__(self, base_url, username, token):
        self.base_url = base_url.rstrip("/")
        self._client = httpx2.Client(
            auth=(username, token),
            headers={"Accept": "application/json"},
            timeout=30.0,
        )

    @classmethod
    def from_env(cls):
        """Build a client from ``CONFLUENCE_URL``/``USERNAME``/``TOKEN`` env vars.

        Raises :class:`click.ClickException` (which prints to stderr and exits
        non-zero) if any are missing.
        """
        base_url = os.environ.get("CONFLUENCE_URL", "").strip().rstrip("/")
        username = os.environ.get("CONFLUENCE_USERNAME", "").strip()
        token = os.environ.get("CONFLUENCE_TOKEN", "").strip()

        missing = [
            name
            for name, value in (
                ("CONFLUENCE_URL", base_url),
                ("CONFLUENCE_USERNAME", username),
                ("CONFLUENCE_TOKEN", token),
            )
            if not value
        ]
        if missing:
            raise click.ClickException(
                f"{', '.join(missing)} must be set in .env or the environment."
            )

        return cls(base_url, username, token)

    def close(self):
        self._client.close()

    def get_page(self, page_id):
        """Fetch current page metadata (title, version number)."""
        resp = self._client.get(f"{self.base_url}/wiki/api/v2/pages/{page_id}")
        resp.raise_for_status()
        return resp.json()

    def search_pages_by_title(self, title):
        """Search Confluence for pages matching the given title exactly."""
        resp = self._client.get(
            f"{self.base_url}/wiki/api/v2/pages",
            params={"title": title, "status": "current"},
        )
        resp.raise_for_status()
        return resp.json().get("results", [])

    def update_page(self, page_id, title, html_body, version, message):
        """Update a Confluence page with new HTML content."""
        payload = {
            "id": page_id,
            "status": "current",
            "title": title,
            "body": {
                "representation": "storage",
                "value": html_body,
            },
            "version": {
                "number": version,
                "message": message,
            },
        }
        resp = self._client.put(
            f"{self.base_url}/wiki/api/v2/pages/{page_id}",
            headers={"Content-Type": "application/json"},
            json=payload,
            timeout=60.0,
        )
        resp.raise_for_status()
        return resp.json()
