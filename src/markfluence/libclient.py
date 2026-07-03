# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""HTTP client for the Confluence REST API.

Wraps an ``httpx2.Client`` with basic auth and the handful of API calls the
CLI needs. Configuration is read from the environment (see
:meth:`ConfluenceClient.from_env`).

Request URLs are built as absolute URLs off ``base_url`` (rather than relying on
``httpx2``'s relative-URL joining against a ``base_url`` path), matching how the
original ``confluence_publish.py`` script constructed them.
"""

import hashlib
import mimetypes
import os
import time

import click
import httpx2

# Prefix under which we stash a file's checksum in the attachment's comment, so a
# later run can tell whether the local image changed. Mirrors mark's approach.
ATTACHMENT_CHECKSUM_PREFIX = "mzcld:checksum: "


def _file_checksum(file_path):
    """Return the hex SHA-256 of a file's contents."""
    digest = hashlib.sha256()
    with open(file_path, "rb") as fh:
        for chunk in iter(lambda: fh.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


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

    def get_page_or_none(self, page_id):
        """Like :meth:`get_page`, but return ``None`` on HTTP 404.

        Used to distinguish "page doesn't exist" from other HTTP errors, which
        are still raised.
        """
        resp = self._client.get(f"{self.base_url}/wiki/api/v2/pages/{page_id}")
        if resp.status_code == 404:
            return None
        resp.raise_for_status()
        return resp.json()

    def page_exists(self, page_id):
        """Return True if a page with this id currently exists."""
        return self.get_page_or_none(page_id) is not None

    def resolve_space_id(self, space_key):
        """Resolve a space key to its numeric space id, or ``None`` if unknown."""
        resp = self._client.get(
            f"{self.base_url}/wiki/api/v2/spaces",
            params={"keys": space_key},
        )
        resp.raise_for_status()
        results = resp.json().get("results", [])
        if not results:
            return None
        return results[0]["id"]

    def search_pages_by_title(self, title, space_id=None):
        """Search Confluence for pages matching the given title exactly.

        When ``space_id`` is given, the search is restricted to that space.
        """
        params = {"title": title, "status": "current"}
        if space_id is not None:
            params["space-id"] = space_id
        resp = self._client.get(
            f"{self.base_url}/wiki/api/v2/pages",
            params=params,
        )
        resp.raise_for_status()
        return resp.json().get("results", [])

    def create_page(self, space_id, title, html_body, parent_id=None):
        """Create a new page in ``space_id`` with storage-format ``html_body``.

        When ``parent_id`` is given the page is created as its child; otherwise
        it is created at the top level of the space.
        """
        payload = {
            "spaceId": space_id,
            "status": "current",
            "title": title,
            "body": {
                "representation": "storage",
                "value": html_body,
            },
        }
        if parent_id is not None:
            payload["parentId"] = parent_id
        resp = self._client.post(
            f"{self.base_url}/wiki/api/v2/pages",
            headers={"Content-Type": "application/json"},
            json=payload,
            timeout=60.0,
        )
        resp.raise_for_status()
        return resp.json()

    # --- Attachments -----------------------------------------------------
    # Attachment write operations only exist in the v1 REST API (v2 is
    # read-only for attachments), so these use /wiki/rest/api/... paths.

    def list_attachments(self, page_id):
        """List a page's attachments, with the checksum comment expanded."""
        resp = self._client.get(
            f"{self.base_url}/wiki/rest/api/content/{page_id}/child/attachment",
            params={"expand": "metadata.comment", "limit": 250},
        )
        resp.raise_for_status()
        return resp.json().get("results", [])

    def create_attachment(self, page_id, filename, comment, file_path, content_type):
        """Create a new attachment on a page (v1 multipart upload)."""
        with open(file_path, "rb") as fh:
            resp = self._client.post(
                f"{self.base_url}/wiki/rest/api/content/{page_id}/child/attachment",
                headers={"X-Atlassian-Token": "nocheck"},
                data={"comment": comment, "minorEdit": "true"},
                files={"file": (filename, fh, content_type)},
                timeout=120.0,
            )
        resp.raise_for_status()
        return resp.json()

    def update_attachment(
        self, page_id, attachment_id, filename, comment, file_path, content_type
    ):
        """Upload a new version of an existing attachment (v1 multipart)."""
        with open(file_path, "rb") as fh:
            resp = self._client.post(
                f"{self.base_url}/wiki/rest/api/content/{page_id}"
                f"/child/attachment/{attachment_id}/data",
                headers={"X-Atlassian-Token": "nocheck"},
                data={"comment": comment, "minorEdit": "true"},
                files={"file": (filename, fh, content_type)},
                timeout=120.0,
            )
        resp.raise_for_status()
        return resp.json()

    def sync_attachments(self, page_id, attachments):
        """Create/update/skip attachments so the page matches the local files.

        ``attachments`` is a list of ``{"path", "filename"}``. Each file's SHA-256
        is stored in the attachment comment; on a later run an unchanged file is
        skipped and a changed one is updated in place (stable filename). Returns a
        list of ``(filename, action)`` where action is created/updated/skipped.
        """
        if not attachments:
            return []

        remote = {a["title"]: a for a in self.list_attachments(page_id)}
        actions = []
        for att in attachments:
            filename = att["filename"]
            path = att["path"]
            comment = ATTACHMENT_CHECKSUM_PREFIX + _file_checksum(path)
            content_type = (
                mimetypes.guess_type(filename)[0] or "application/octet-stream"
            )

            existing = remote.get(filename)
            if existing is None:
                self.create_attachment(page_id, filename, comment, path, content_type)
                actions.append((filename, "created"))
            elif existing.get("metadata", {}).get("comment", "") == comment:
                actions.append((filename, "skipped"))
            else:
                self.update_attachment(
                    page_id, existing["id"], filename, comment, path, content_type
                )
                actions.append((filename, "updated"))
        return actions

    def get_user(self, account_id):
        """Look up a user's display name by account id (best-effort).

        Uses the v1 user endpoint since v2 has no clean by-id user fetch.
        Returns the display name, or ``None`` if the lookup fails for any
        reason -- callers fall back to showing the raw account id.
        """
        if not account_id:
            return None
        try:
            resp = self._client.get(
                f"{self.base_url}/wiki/rest/api/user",
                params={"accountId": account_id},
            )
            resp.raise_for_status()
        except httpx2.HTTPError:
            return None
        return resp.json().get("displayName")

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

    # --- Content properties ----------------------------------------------
    # Page appearance (e.g. width) is stored as content properties rather than
    # on the page body, so setting it is a separate get-then-create-or-update.

    def get_content_property(self, page_id, key):
        """Return a page's content property matching ``key``, or ``None``.

        The returned dict includes ``value`` and ``version.number`` (needed to
        update it).
        """
        resp = self._client.get(
            f"{self.base_url}/wiki/api/v2/pages/{page_id}/properties",
            params={"key": key},
        )
        resp.raise_for_status()
        results = resp.json().get("results", [])
        return results[0] if results else None

    def set_content_property(self, page_id, key, value):
        """Idempotently set a content property. Returns "set" or "unchanged".

        Creates the property if absent, updates it (version-bumped) if it
        differs, and does nothing if it already equals ``value``. Content-
        property writes around a page write are known to occasionally return a
        spurious 4xx/5xx even though they applied, so on any HTTP error this
        pauses and retries once (the retry re-reads first, so an actually-
        applied write resolves to "unchanged"). Raises if the retry also fails.
        """
        for attempt in (1, 2):
            try:
                existing = self.get_content_property(page_id, key)
                if existing is not None and existing.get("value") == value:
                    return "unchanged"
                if existing is not None:
                    self._client.put(
                        f"{self.base_url}/wiki/api/v2/pages/{page_id}"
                        f"/properties/{existing['id']}",
                        headers={"Content-Type": "application/json"},
                        json={
                            "key": key,
                            "value": value,
                            "version": {"number": existing["version"]["number"] + 1},
                        },
                    ).raise_for_status()
                else:
                    self._client.post(
                        f"{self.base_url}/wiki/api/v2/pages/{page_id}/properties",
                        headers={"Content-Type": "application/json"},
                        json={"key": key, "value": value},
                    ).raise_for_status()
                return "set"
            except httpx2.HTTPError:
                if attempt == 1:
                    time.sleep(1)
                    continue
                raise
