#!/usr/bin/env python3
"""Tests for scripts/gen-postman.py (ENT-265).

    python3 scripts/test_gen_postman.py

Plain asserts and no test runner, because this has to run in the CI job that
regenerates the collection, and that job has Go and buf and nothing else. A
`pip install pytest` there to run four tests would be a worse trade than a
thirty-line harness.

WHAT IS ACTUALLY UNDER TEST

The drift check in CI already proves the generator is idempotent on the
committed file, and proves it on every run. What it cannot prove is the
property everything else rests on: that the reader and printer are faithful to
input they were never asked to change. AGENTS.md's third rule about this
collection is that a library round trip expands the compact arrays and escapes
the section signs, so a generator that re-dumps is the failure, not the fix.
So the tests below feed the printer input that a naive re-dump mangles, and
assert the bytes come back.

The merge tests cover the two cases the drift check cannot reach from a
committed collection that is already in sync: an RPC arriving, and an RPC
leaving.
"""

from __future__ import annotations

import importlib.util
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("gen_postman", os.path.join(HERE, "gen-postman.py"))
gen = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(gen)


FAILURES: list[str] = []


def check(name: str, condition: bool, detail: str = "") -> None:
    if condition:
        print(f"  ok   {name}")
        return
    FAILURES.append(name)
    print(f"  FAIL {name}" + (f"\n       {detail}" if detail else ""))


def method(name="ListWidgets", service="WidgetService", package="kindlast.core.v1"):
    return gen.Method(
        package=package,
        service=service,
        name=name,
        proto_file="proto/kindlast/core/v1/widget.proto",
        scope="widgets:read",
        binding="GET /api/v1/widgets",
        comment="Every widget.",
    )


# --------------------------------------------------------------------------
# The printer keeps what it did not change.
# --------------------------------------------------------------------------


def test_round_trip_is_byte_identical() -> None:
    # Every shape the committed collection actually contains, including the
    # three a width-based printer gets wrong. In order: an object the author
    # left expanded although it fits on one line, an array the same, an object
    # kept on one line although it is over eighty columns, and a string written
    # with an escaped section sign where nineteen others carry a literal one.
    source = (
        "{\n"
        '  "expanded": {\n'
        '    "a": 1\n'
        "  },\n"
        '  "array": [\n'
        '    "one"\n'
        "  ],\n"
        '  "wide": { "key": "Content-Type", "value": "application/x-www-form-urlencoded" },\n'
        '  "escaped": "see \\u00a712 and \\u00a723",\n'
        '  "literal": "see \u00a712 and \u00a723",\n'
        '  "compact": ["a", "b"],\n'
        '  "empty": {},\n'
        '  "types": [true, false, null, 1.5]\n'
        "}\n"
    )
    printed = gen.write_document(gen.read_document(source))
    check(
        "round trip is byte identical",
        printed == source,
        f"got:\n{printed}\nwant:\n{source}",
    )


def test_round_trip_of_the_committed_collection() -> None:
    with open(gen.COLLECTION, encoding="utf-8") as handle:
        original = handle.read()
    printed = gen.write_document(gen.read_document(original))
    check("the committed collection round trips unchanged", printed == original)


def test_the_round_trip_test_can_fail() -> None:
    # A test that cannot fail is worse than no test (AGENTS.md). This breaks
    # the preservation deliberately, the way a re-dump would, and asserts the
    # printer notices.
    source = '{\n  "expanded": {\n    "a": 1\n  }\n}\n'
    document = gen.read_document(source)
    document.value["expanded"].multiline = None  # what a width-only printer does
    check(
        "dropping the preserved layout changes the bytes",
        gen.write_document(document) != source,
    )


# --------------------------------------------------------------------------
# The merge.
# --------------------------------------------------------------------------


def collection_with(items: str) -> gen.Node:
    return gen.read_document(
        "{\n"
        '  "item": [\n'
        "    {\n"
        '      "name": "Auth (Zitadel)",\n'
        '      "item": [{ "name": "hand written", "request": { "method": "GET" } }]\n'
        "    },\n"
        "    {\n"
        f'      "name": "{gen.FOLDER}",\n'
        f'      "item": [{items}]\n'
        "    }\n"
        "  ]\n"
        "}\n"
    )


def core_items(document: gen.Node) -> list[gen.Node]:
    for folder in document.value["item"].value:
        if folder.value["name"].value == gen.FOLDER:
            return folder.value["item"].value
    raise AssertionError("no core folder")


def test_a_new_rpc_produces_a_request() -> None:
    document = collection_with("")
    changes = gen.merge(document, [method()])
    items = core_items(document)
    request = items[0].value["request"].value
    check("a new RPC produces exactly one request", len(items) == 1 and len(changes) == 1)
    check(
        "the new request calls the Connect path",
        request["url"].value["raw"].value
        == "{{api_base_url}}/kindlast.core.v1.WidgetService/ListWidgets",
    )
    check("the new request is a POST", request["method"].value == "POST")
    check(
        "the new request carries the proto comment and the contract block",
        request["description"].value.startswith("Every widget.")
        and gen.MARKER in request["description"].value
        and "`widgets:read`" in request["description"].value,
    )


def test_an_rpc_that_is_gone_takes_its_request_with_it() -> None:
    document = collection_with(
        '\n        { "name": "Old", "request": { "method": "POST", "url": '
        '{ "raw": "{{api_base_url}}/kindlast.core.v1.WidgetService/Removed" } } }\n      '
    )
    gen.merge(document, [method()])
    names = [item.value["name"].value for item in core_items(document)]
    check("a removed RPC loses its request", "Old" not in names)
    check("and the replacement is there", names == ["ListWidgets (WidgetService)"], str(names))


def test_hand_written_prose_survives_and_the_block_does_not_double() -> None:
    document = collection_with("")
    gen.merge(document, [method()])
    request = core_items(document)[0].value["request"].value
    # What a human does to a generated request: replaces the prose above the
    # marker with something measured, and leaves the block below it alone.
    request["description"] = gen.node(
        "Measured on this stack: the audience is the project id.\n\n"
        + gen.contract_block(method())
    )
    before = request["description"].value
    gen.merge(document, [method()])
    after = core_items(document)[0].value["request"].value["description"].value
    check("prose above the marker survives", after.startswith("Measured on this stack"))
    check("the contract block appears once", after.count(gen.MARKER) == 1)
    check("a second run changes nothing", after == before)


def test_a_request_served_by_another_process_keeps_its_host() -> None:
    document = collection_with(
        '\n        { "name": "Gateway", "request": { "method": "POST", "url": '
        '{ "raw": "{{gateway_base_url}}/kindlast.core.v1.WidgetService/ListWidgets" } } }\n      '
    )
    gen.merge(document, [method()])
    raw = core_items(document)[0].value["request"].value["url"].value["raw"].value
    check(
        "the host is preserved, only the procedure is rewritten",
        raw == "{{gateway_base_url}}/kindlast.core.v1.WidgetService/ListWidgets",
        raw,
    )


def test_hand_written_folders_are_untouched() -> None:
    document = collection_with("")
    gen.merge(document, [method()])
    auth = document.value["item"].value[0]
    check(
        "the hand-written folder is unchanged",
        gen.write_document(auth)
        == '{\n  "name": "Auth (Zitadel)",\n'
        '  "item": [{ "name": "hand written", "request": { "method": "GET" } }]\n}\n',
        gen.write_document(auth),
    )


def main() -> int:
    for name, test in sorted(globals().items()):
        if name.startswith("test_") and callable(test):
            print(name)
            test()
    if FAILURES:
        print(f"\n{len(FAILURES)} failure(s): " + ", ".join(FAILURES))
        return 1
    print("\nall good")
    return 0


if __name__ == "__main__":
    sys.exit(main())
