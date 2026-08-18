"""This service holds no database credential and opens no connection (§1.6).

ENT-218 lists it as an acceptance criterion, and it is worth a test rather than
a code review habit because it is the kind of property that stays true right up
until somebody adds one import to solve a real problem quickly.

The split it protects: Go loads and Go persists; Python drafts and returns.
Intelligence therefore has no tenancy GUCs to set and no RLS session to be
wrong about, which is what makes "a human can never reach it" a structural
claim rather than a promise. Give it a connection and the claim quietly stops
being true, because now there is something for a confused-deputy bug to reach.
"""

from __future__ import annotations

import tomllib
from pathlib import Path

PYPROJECT = Path(__file__).resolve().parent.parent / "pyproject.toml"

# Anything that can open a connection to a database. Names rather than import
# probes, because the point is that the dependency is not declared at all: a
# package that is absent cannot be imported by accident later.
DATABASE_PACKAGES = {
    "psycopg",
    "psycopg2",
    "psycopg2-binary",
    "asyncpg",
    "sqlalchemy",
    "sqlmodel",
    "databases",
    "aiopg",
    "pg8000",
    "peewee",
    "tortoise-orm",
    "alembic",
}


def _declared_dependencies() -> set[str]:
    manifest = tomllib.loads(PYPROJECT.read_text())
    declared: list[str] = list(manifest["project"].get("dependencies", []))
    for group in manifest.get("dependency-groups", {}).values():
        declared.extend(g for g in group if isinstance(g, str))

    names = set()
    for spec in declared:
        # "pyjwt>=2.13.0" -> "pyjwt". Enough for a name check; this is not a
        # requirements parser and does not need to be.
        name = spec.split(";")[0]
        for sep in (">=", "<=", "==", "~=", "!=", ">", "<", "["):
            name = name.split(sep)[0]
        names.add(name.strip().lower())
    return names


def test_no_database_driver_is_declared():
    declared = _declared_dependencies()
    found = declared.intersection(DATABASE_PACKAGES)

    assert not found, (
        f"{sorted(found)} would give this service a database connection. "
        "It must not have one: Go loads and Go persists, Python drafts and "
        "returns (§1.6). If a genuine need has appeared, it is a design "
        "change to raise, not a dependency to add."
    )


def test_no_source_file_mentions_a_connection_string():
    """A DSN in the source is the same problem arriving by another door.

    Catches the case where somebody reaches the database through a package
    that is already here for another reason, or through a raw socket.
    """
    src = Path(__file__).resolve().parent.parent / "src"
    offenders = []

    for path in src.rglob("*.py"):
        text = path.read_text().lower()
        for marker in ("postgres://", "postgresql://", "dbname=", "kindlast_app"):
            if marker in text:
                offenders.append(f"{path.name}: {marker}")

    assert not offenders, (
        f"connection-string markers found: {offenders}. This service has no "
        "database credential by design."
    )
