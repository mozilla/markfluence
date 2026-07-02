"""Command-line entry point for mzcld-confluence-cli."""

import click
from dotenv import load_dotenv

from .update import update


@click.group()
def main():
    """Publish and manipulate Confluence pages and attachments."""
    load_dotenv()


main.add_command(update)


if __name__ == "__main__":
    main()
