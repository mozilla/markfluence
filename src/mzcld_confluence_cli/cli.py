# This Source Code Form is subject to the terms of the Mozilla Public
# License, v. 2.0. If a copy of the MPL was not distributed with this
# file, You can obtain one at https://mozilla.org/MPL/2.0/.

"""Command-line entry point for mzcld-confluence-cli."""

import click
from dotenv import find_dotenv, load_dotenv

from .create import create
from .update import update

load_dotenv(find_dotenv(usecwd=True))


@click.group()
def main():
    """Publish and manipulate Confluence pages and attachments."""


main.add_command(create)
main.add_command(update)


if __name__ == "__main__":
    main()
