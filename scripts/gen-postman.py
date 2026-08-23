#!/usr/bin/env python3
"""Regenerate the contract half of the Postman collection (ENT-265).

WHAT THIS OWNS, AND WHY IT DOES NOT OWN MORE

AGENTS.md asks every author to mirror an API change into `postman/` by hand,
and enforces it only by asking. That is the rule this script replaces for the
part a machine can check: every RPC in `proto/` has a request in the
`Core API v1` folder, at the path and with the scope the contract declares.
CI regenerates and fails on any diff, the way it already does for `gen/`.

It deliberately does not own the whole request. The proto contract does not
carry enough to produce a working one:

  * Which calls need the `Kindlast-Org-Id` header is not in the contract and
    is not derivable from the package. Seven requests contradict the obvious
    rule, in both directions.
  * A useful body carries realistic values (`{{finding_id}}`, an actual enum
    member). A skeleton generated from the message schema is valid and
    useless.
  * The descriptions carry facts that were measured against a running stack
    and exist nowhere else, which is the second of the three things AGENTS.md
    says to get right about this collection.

So a request that already exists keeps its name, headers, body and prose, and
this script rewrites only the contract facts inside it. A generated collection
that is complete and does not work would be worse than the hand-written one it
replaced: AGENTS.md's warning is that a collection which is merely stale is
worse than none, because somebody will believe it.

WHY THE PROTO IMAGE RATHER THAN gen/openapi/openapi.yaml

The issue asked for the OpenAPI document, and the OpenAPI document describes a
surface nothing serves. `gen/openapi/openapi.yaml` is generated from the
`google.api.http` annotations, so its paths are `GET /api/v1/me` and the rest
of the REST binding. Neither core-api nor the Caddy edge routes those: the edge
forwards `/kindlast.core.v1.*` and `/kindlast.platform.v1.*` to the Connect
handlers, and opens exactly one `/api/v1` path, for the billing webhook.
Opening the REST surface is ENT-193's decision, with a gateway and rate
limiting attached to it.

Generating the requests from that document would therefore have replaced
every working Core API request with one that 404s, which is the exact
failure the collection exists to prevent. So the source here is the buf image,
which is what the OpenAPI document is generated from as well. It carries the
proto package (so the Connect path can be built), the `required_scope` option
and the leading comments, none of which survive into the OpenAPI document.

NOTHING OUTSIDE THE FOLDER MOVES, INCLUDING WHITESPACE

The third thing AGENTS.md says to get right: loading the collection and
re-dumping it through a JSON library expands the compact arrays and escapes
the section signs, turning a six-line change into a two-hundred-line diff
nobody can review. So this does not re-dump. It reads the file into a tree
that remembers, for every object and array, whether the source had it on one
line, and for every string, the exact bytes it was written with, and prints
those back unchanged. Run against an unmodified checkout, the output is byte
for byte the input. `test_gen_postman.py` asserts that, because a formatter
that is only usually faithful is one nobody will run.

USAGE

    ./scripts/gen-postman.py            # rewrite the collection in place
    ./scripts/gen-postman.py --check    # exit 1 if it would change anything

Needs `buf` on PATH, at the version CI pins (see .github/workflows/ci.yml).
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import tempfile

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
COLLECTION = os.path.join(REPO_ROOT, "postman", "kindlast.postman_collection.json")

# The folder this script owns. Everything else in the collection is
# hand-written and is not read, let alone written.
FOLDER = "Core API v1"

# Where the generated half of a description begins. A visible sentence rather
# than an HTML comment, because Postman renders descriptions as markdown and a
# marker a reader cannot see is one they will edit across without meaning to.
MARKER = "**From the contract.**"

# Prettier's default, which is what the committed collection is formatted to.
PRINT_WIDTH = 80


# --------------------------------------------------------------------------
# A JSON reader and printer that preserves the formatting of what it did not
# change.
#
# Two facts about the committed file drive this. Prettier keeps an object or
# array on one line if the source had it on one line, and breaks it if the
# source broke it, so width alone does not reproduce the file: there are
# eighty-five-column lines in it that Prettier left alone. And three strings
# were written with `§` escapes where nineteen others carry a literal
# section sign, which is the fingerprint of exactly the library re-dump
# AGENTS.md warns about. Remembering the source bytes of every string makes
# both irrelevant.
# --------------------------------------------------------------------------


class Node:
    """A JSON value, plus how it was written.

    `multiline` is True or False for a value that came from the file and None
    for one this script built, which is the signal to lay it out by width.
    `raw` is the source text of a string, or None for a string this script
    built.
    """

    __slots__ = ("value", "multiline", "raw")

    def __init__(self, value, multiline=None, raw=None):
        self.value = value
        self.multiline = multiline
        self.raw = raw


def node(value):
    """Wrap a plain Python value as a new node, recursively."""
    if isinstance(value, Node):
        return value
    if isinstance(value, dict):
        return Node({k: node(v) for k, v in value.items()})
    if isinstance(value, list):
        return Node([node(v) for v in value])
    return Node(value)


class _Parser:
    def __init__(self, text: str):
        self.text = text
        self.i = 0

    def _skip_space(self) -> None:
        while self.i < len(self.text) and self.text[self.i] in " \t\r\n":
            self.i += 1

    def _broke_after_bracket(self) -> bool:
        j = self.i
        while j < len(self.text) and self.text[j] in " \t\r":
            j += 1
        return j < len(self.text) and self.text[j] == "\n"

    def parse(self) -> Node:
        self._skip_space()
        char = self.text[self.i]
        if char == "{":
            return self._object()
        if char == "[":
            return self._array()
        if char == '"':
            start = self.i
            value = self._string()
            return Node(value, False, self.text[start : self.i])
        return self._literal()

    def _object(self) -> Node:
        self.i += 1
        multiline = self._broke_after_bracket()
        out: dict[str, Node] = {}
        self._skip_space()
        if self.text[self.i] == "}":
            self.i += 1
            return Node(out, False)
        while True:
            self._skip_space()
            key = self._string()
            self._skip_space()
            assert self.text[self.i] == ":", "expected ':' after an object key"
            self.i += 1
            out[key] = self.parse()
            self._skip_space()
            if self.text[self.i] == ",":
                self.i += 1
                continue
            assert self.text[self.i] == "}", "expected ',' or '}' in an object"
            self.i += 1
            return Node(out, multiline)

    def _array(self) -> Node:
        self.i += 1
        multiline = self._broke_after_bracket()
        out: list[Node] = []
        self._skip_space()
        if self.text[self.i] == "]":
            self.i += 1
            return Node(out, False)
        while True:
            out.append(self.parse())
            self._skip_space()
            if self.text[self.i] == ",":
                self.i += 1
                continue
            assert self.text[self.i] == "]", "expected ',' or ']' in an array"
            self.i += 1
            return Node(out, multiline)

    def _string(self) -> str:
        start = self.i
        self.i += 1
        while True:
            char = self.text[self.i]
            if char == "\\":
                self.i += 2
                continue
            self.i += 1
            if char == '"':
                return json.loads(self.text[start : self.i])

    def _literal(self) -> Node:
        start = self.i
        while self.i < len(self.text) and self.text[self.i] not in ",}] \t\r\n":
            self.i += 1
        return Node(json.loads(self.text[start : self.i]), False)


def _quote(text: str) -> str:
    return json.dumps(text, ensure_ascii=False)


def _flat(item: Node) -> str:
    if item.raw is not None:
        return item.raw
    value = item.value
    if isinstance(value, dict):
        if not value:
            return "{}"
        inner = ", ".join(_quote(k) + ": " + _flat(v) for k, v in value.items())
        return "{ " + inner + " }"
    if isinstance(value, list):
        if not value:
            return "[]"
        return "[" + ", ".join(_flat(v) for v in value) + "]"
    if isinstance(value, str):
        return _quote(value)
    if value is True:
        return "true"
    if value is False:
        return "false"
    if value is None:
        return "null"
    return json.dumps(value)


def _print(item: Node, indent: int, column: int) -> str:
    value = item.value
    if not isinstance(value, (dict, list)):
        return _flat(item)
    if item.multiline is False:
        return _flat(item)
    if item.multiline is None:
        flat = _flat(item)
        if "\n" not in flat and column + len(flat) <= PRINT_WIDTH:
            return flat
    pad = " " * (indent + 2)
    if isinstance(value, dict):
        if not value:
            return "{}"
        parts = [
            pad
            + _quote(key)
            + ": "
            + _print(child, indent + 2, indent + 2 + len(_quote(key)) + 2)
            for key, child in value.items()
        ]
        return "{\n" + ",\n".join(parts) + "\n" + " " * indent + "}"
    if not value:
        return "[]"
    parts = [pad + _print(child, indent + 2, indent + 2) for child in value]
    return "[\n" + ",\n".join(parts) + "\n" + " " * indent + "]"


def read_document(text: str) -> Node:
    return _Parser(text).parse()


def write_document(root: Node) -> str:
    return _print(root, 0, 0) + "\n"


# --------------------------------------------------------------------------
# The contract.
# --------------------------------------------------------------------------


class Method:
    __slots__ = ("package", "service", "name", "proto_file", "scope", "binding", "comment")

    def __init__(self, package, service, name, proto_file, scope, binding, comment):
        self.package = package
        self.service = service
        self.name = name
        self.proto_file = proto_file
        self.scope = scope
        self.binding = binding
        self.comment = comment

    @property
    def procedure(self) -> str:
        """The Connect path, which is the fully qualified name and the method."""
        return f"{self.package}.{self.service}/{self.name}"


# The HTTP verbs google.api.HttpRule can name. `custom` is deliberately absent:
# nothing here declares one, and guessing at a shape no proto uses would be a
# branch no test could cover.
_HTTP_VERBS = ("get", "put", "post", "delete", "patch")


def _leading_comment(source_info: dict, path: list[int]) -> str:
    """The comment written above a declaration, as markdown.

    protoc hands back each line with the single space that followed `//` still
    attached, and a trailing newline on the last line. Both go.

    The hard wraps go too, and paragraphs join onto one line each. A proto
    comment is wrapped to seventy-odd columns for someone reading the proto;
    every description already in this collection is one long line per
    paragraph, because that is what Postman's editor and a JSON diff both want.
    Blank lines survive, since they are what separates the paragraphs.
    """
    for location in source_info.get("location", []):
        if location.get("path") != path or "leadingComments" not in location:
            continue
        lines = location["leadingComments"].split("\n")
        if lines and lines[-1] == "":
            lines.pop()
        lines = [line[1:] if line.startswith(" ") else line for line in lines]
        paragraphs = []
        current: list[str] = []
        for line in lines:
            if line.strip():
                current.append(line.strip())
            elif current:
                paragraphs.append(" ".join(current))
                current = []
        if current:
            paragraphs.append(" ".join(current))
        return "\n\n".join(paragraphs).strip()
    return ""


def read_contract(image_path: str) -> list[Method]:
    with open(image_path, encoding="utf-8") as handle:
        image = json.load(handle)

    methods: list[Method] = []
    for descriptor in image["file"]:
        package = descriptor.get("package", "")
        if not package.startswith("kindlast."):
            continue
        source_info = descriptor.get("sourceCodeInfo", {})
        for service_index, service in enumerate(descriptor.get("service", [])):
            for method_index, method in enumerate(service.get("method", [])):
                options = method.get("options", {})
                rule = options.get("[google.api.http]", {})
                binding = ""
                for verb in _HTTP_VERBS:
                    if verb in rule:
                        binding = f"{verb.upper()} {rule[verb]}"
                        break
                methods.append(
                    Method(
                        package=package,
                        service=service["name"],
                        name=method["name"],
                        proto_file="proto/" + descriptor["name"],
                        scope=options.get("[kindlast.options.v1.required_scope]", ""),
                        binding=binding,
                        # 6 is FileDescriptorProto.service, 2 is
                        # ServiceDescriptorProto.method. The path is how
                        # sourceCodeInfo names a declaration, and there is no
                        # friendlier accessor in the JSON form of an image.
                        comment=_leading_comment(source_info, [6, service_index, 2, method_index]),
                    )
                )
    return methods


def build_image(destination: str) -> None:
    try:
        subprocess.run(
            ["buf", "build", "-o", destination],
            cwd=REPO_ROOT,
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
    except FileNotFoundError:
        sys.exit(
            "buf is not on PATH. Install the version CI pins:\n"
            "  go install github.com/bufbuild/buf/cmd/buf@v1.72.0"
        )
    except subprocess.CalledProcessError as error:
        sys.exit("buf build failed:\n" + error.stderr.decode("utf-8", "replace"))


# --------------------------------------------------------------------------
# The merge.
# --------------------------------------------------------------------------


def contract_block(method: Method) -> str:
    """The generated tail of a request description.

    Short on purpose. It carries the three facts that break a caller when they
    change and that a reader cannot recover from the request itself: which RPC
    this is, what scope a token has to hold, and where to go to argue with any
    of it. The REST binding is here because it is part of the contract and a
    client generated from `gen/openapi/openapi.yaml` will use it; the folder
    description says once, rather than once per request, that nothing routes it
    yet.
    """
    parts = [
        f"{MARKER} `{method.procedure}`, declared in `{method.proto_file}`.",
        f"Required scope: `{method.scope}`." if method.scope else "No scope declared.",
    ]
    if method.binding:
        parts.append(f"Declared REST binding: `{method.binding}`.")
    return " ".join(parts)


def merge_description(existing: str, method: Method) -> str:
    """Hand-written prose above the marker, generated facts below it.

    An RPC that has no request yet gets its proto comment as the prose, so a
    new endpoint arrives described rather than bare. An RPC that already has
    one keeps whatever a human wrote, which is more than the proto comment says
    in every case here, and would be lost by overwriting it with the comment.
    Once written, the prose is hand-owned; the block below the marker is not.
    """
    prose = existing.split("\n\n" + MARKER)[0].rstrip()
    if not prose:
        prose = method.comment
    block = contract_block(method)
    return (prose + "\n\n" + block) if prose else block


# Where a request goes when nothing says otherwise: core-api, through the
# Caddy edge, which is the only door it has.
DEFAULT_HOST = "{{api_base_url}}"


def url_node(method: Method, host: str) -> Node:
    """The Connect URL: the caller's base address, then the procedure.

    THE HOST IS NOT A CONTRACT FACT, which is worth stating because the first
    run of this script tried to make it one and moved two requests to the wrong
    process. `kindlast.platform.v1` is a package, not a deployment: core-api
    serves most of it, `apps/workers` serves GatewayService on
    `gateway_base_url`, and the Python service serves IntelligenceService on
    `intelligence_base_url`. Nothing in the proto says which, so the host of an
    existing request is preserved and only the procedure below it is rewritten.
    """
    built = Node(
        {
            "raw": Node(host + "/" + method.procedure, False),
            "host": Node([Node(host, False)], False),
            "path": Node(
                [Node(f"{method.package}.{method.service}", False), Node(method.name, False)],
                False,
            ),
        },
        True,
    )
    return built


def host_of(item: Node) -> str:
    """The base address an existing request uses, or the default."""
    raw = item.value["request"].value["url"].value["raw"].value
    marker = "/kindlast."
    return raw[: raw.index(marker)] if marker in raw else DEFAULT_HOST


def new_request(method: Method) -> Node:
    """A request for an RPC that has none yet.

    The headers are a starting point and nothing more: which calls need the
    active-organisation header is not in the contract, so this guesses from the
    package and a human corrects it. The guess is preserved once corrected,
    because this script never rewrites headers on a request that exists.
    """
    headers = [
        {"key": "Content-Type", "value": "application/json"},
        {"key": "Authorization", "value": "Bearer {{access_token}}"},
    ]
    if method.package.startswith("kindlast.core."):
        headers.append({"key": "Kindlast-Org-Id", "value": "{{org_id}}"})

    return node(
        {
            "name": f"{method.name} ({method.service})",
            "request": {
                "method": "POST",
                "header": headers,
                "body": {"mode": "raw", "raw": "{}"},
                "url": url_node(method, DEFAULT_HOST),
                "description": merge_description("", method),
            },
        }
    )


def procedure_of(item: Node) -> str:
    """The RPC an existing request calls, or "" if it calls something else.

    Read off the URL rather than the name, because the name is hand-written
    prose and two requests deliberately share one RPC (the finding approval has
    a second example for an agent acting for a person).
    """
    request = item.value.get("request")
    if request is None:
        return ""
    url = request.value.get("url")
    if url is None:
        return ""
    raw = url.value.get("raw")
    if raw is None or not isinstance(raw.value, str):
        return ""
    marker = "/kindlast."
    if marker not in raw.value:
        return ""
    return raw.value[raw.value.index(marker) + 1 :]


def merge(root: Node, methods: list[Method]) -> list[str]:
    """Rewrite the folder in place. Returns a line per change, for --check."""
    folders = [
        f
        for f in root.value["item"].value
        if "name" in f.value and f.value["name"].value == FOLDER
    ]
    if len(folders) != 1:
        sys.exit(f"expected exactly one {FOLDER!r} folder, found {len(folders)}")
    items = folders[0].value["item"].value

    by_procedure = {method.procedure: method for method in methods}
    changes: list[str] = []

    # Requests naming an RPC the contract no longer declares. Removed rather
    # than reported, because a request to an endpoint that is gone is the kind
    # of stale the collection exists to prevent; git history keeps the prose.
    kept = []
    for item in items:
        procedure = procedure_of(item)
        if procedure and procedure not in by_procedure:
            changes.append(f"removed {item.value['name'].value!r}: {procedure} is not in the contract")
            continue
        kept.append(item)
    items[:] = kept

    # Existing requests: the contract facts, and nothing else.
    seen: set[str] = set()
    for item in items:
        procedure = procedure_of(item)
        if not procedure:
            continue
        seen.add(procedure)
        method = by_procedure[procedure]
        request = item.value["request"].value
        name = item.value["name"].value

        if request["method"].value != "POST":
            changes.append(f"{name}: method -> POST")
            request["method"] = node("POST")

        wanted_url = url_node(method, host_of(item))
        if _flat(request["url"]) != _flat(wanted_url):
            changes.append(f"{name}: url -> {wanted_url.value['raw'].value}")
            request["url"] = wanted_url

        existing = request["description"].value if "description" in request else ""
        merged = merge_description(existing, method)
        if merged != existing:
            changes.append(f"{name}: description block")
            request["description"] = node(merged)

    # RPCs with no request. Inserted after the last request of the same
    # service, so a new method lands next to its siblings rather than at the
    # end of eighty-odd requests, and the position is derived rather than
    # chosen, so two runs agree.
    for method in methods:
        if method.procedure in seen:
            continue
        prefix = f"{method.package}.{method.service}/"
        insert_at = len(items)
        for index, item in enumerate(items):
            if procedure_of(item).startswith(prefix):
                insert_at = index + 1
        items.insert(insert_at, new_request(method))
        changes.append(f"added a request for {method.procedure}")

    return changes


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    parser.add_argument(
        "--check",
        action="store_true",
        help="do not write; exit 1 if the collection is out of date",
    )
    args = parser.parse_args()

    with tempfile.TemporaryDirectory() as workspace:
        image = os.path.join(workspace, "image.json")
        build_image(image)
        methods = read_contract(image)

    if not methods:
        sys.exit("the proto image declares no methods; refusing to empty the folder")

    with open(COLLECTION, encoding="utf-8") as handle:
        original = handle.read()

    root = read_document(original)
    changes = merge(root, methods)
    updated = write_document(root)

    if updated == original:
        print(f"{FOLDER}: up to date, {len(methods)} RPCs")
        return 0

    if args.check:
        print("The Postman collection is out of date. Changes it needs:")
        for change in changes:
            print("  " + change)
        print("\nRun ./scripts/gen-postman.py and commit the result.")
        return 1

    with open(COLLECTION, "w", encoding="utf-8") as handle:
        handle.write(updated)
    print(f"{FOLDER}: rewrote {len(changes)} thing(s) across {len(methods)} RPCs")
    for change in changes:
        print("  " + change)
    return 0


if __name__ == "__main__":
    sys.exit(main())
