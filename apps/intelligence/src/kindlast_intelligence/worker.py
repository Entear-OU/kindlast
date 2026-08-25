"""The Temporal worker: this service's half of the `intelligence` task queue
(ENT-256, part five; core-api-surface §16.4).

# WHAT RUNS HERE

Three activities, each the same run the RPC of its name performs: the harness,
the guardrail ring, whatever validator that skill has, and the run record. The
Go workflows load, this service drafts or decides, and Go persists, each half
retrying on its own.

  DraftNarrative   the Analyst. The sweep workflow asks core-api for the next
                   finding with no narrative and records what comes back.
  Watch            the Watcher, over one organisation's context.
  DraftMessage     the Messenger (ENT-260). The doorbell workflow runs it
                   between planning who to tell and telling them, and treats
                   anything other than a drafted message as "use the
                   template", so a notification goes out either way.

# WHAT THIS WORKER HOLDS, WHICH IS THE SAME AS THE RPC: NOTHING

No database handle, no tenancy GUC, no model endpoint, no credential. Every
model call goes back through core-api's CompletionService bound to the
organisation the draft is for, and the run is recorded through core-api's
RecordAgentRun, with this service's own OAuth client, which is the one
credential it legitimately has. The draft request arrives from the engine,
built by core-api; it is data, and this worker cannot be asked to draft for
an organisation core-api did not name.

# WHY THE ACTIVITY IS SYNCHRONOUS

The harness is synchronous and blocks on the model for seconds to minutes;
Temporal's Python SDK runs such activities on a thread pool, sized here to
what one local model can serve at once (ENT-235: one or two requests), so a
queue of drafts is a queue rather than a pile of timeouts. The RPC half of
this process serves from waitress beside it.
"""

from __future__ import annotations

import asyncio
import logging
import threading
from concurrent.futures import ThreadPoolExecutor

from connectrpc.code import Code
from connectrpc.errors import ConnectError
from kindlast.platform.v1 import intelligence_pb2, watcher_pb2
from temporalio import activity
from temporalio.client import Client
from temporalio.exceptions import ApplicationError
from temporalio.worker import Worker

from .service import IntelligenceService

logger = logging.getLogger("kindlast.intelligence.worker")

# Pinned here and in apps/workers/internal/schedule/narrate.go. The Go
# workflow schedules the activity by this name on this queue; the two must
# agree, which the end-to-end run is what proves.
TASK_QUEUE = "intelligence"
ACTIVITY_NAME = "DraftNarrative"
WATCH_ACTIVITY_NAME = "Watch"
# Pinned here first (ENT-260). The Go half of the pin arrives with the caller:
# the doorbell workflow does not schedule this activity yet, and when it does,
# apps/workers/internal/schedule/doorbell.go must name exactly this string,
# which the end-to-end run is what proves.
DRAFT_MESSAGE_ACTIVITY_NAME = "DraftMessage"


def make_activity(service: IntelligenceService):
    """The activity, closed over the service that drafts."""

    @activity.defn(name=ACTIVITY_NAME)
    def draft_narrative(
        request: intelligence_pb2.DraftNarrativeRequest,
    ) -> intelligence_pb2.DraftNarrativeResponse:
        try:
            return service.draft(request)
        except ConnectError as exc:
            if exc.code == Code.INVALID_ARGUMENT:
                # A malformed request is core-api's bug, not a transient
                # condition; retrying it produces the same refusal. Said so,
                # so the workflow records it and moves on.
                raise ApplicationError(str(exc), type="bad-request", non_retryable=True) from exc
            # Anything else (the run could not be recorded, the model could
            # not be reached through core-api) is worth the workflow's retry
            # policy.
            raise ApplicationError(str(exc), type=exc.code.name) from exc

    return draft_narrative


def make_watch_activity(service: IntelligenceService):
    """The Watcher activity, closed over the service that watches."""

    @activity.defn(name=WATCH_ACTIVITY_NAME)
    def watch(request: intelligence_pb2.WatchRequest) -> intelligence_pb2.WatchResponse:
        try:
            return service.run_watch(request)
        except ConnectError as exc:
            if exc.code == Code.INVALID_ARGUMENT:
                # A malformed request, or an organisation with no profile.
                # Neither changes by waiting: retrying produces the same
                # refusal, so the workflow records it and moves on.
                raise ApplicationError(str(exc), type="bad-request", non_retryable=True) from exc
            raise ApplicationError(str(exc), type=exc.code.name) from exc

    return watch


def make_draft_message_activity(service: IntelligenceService):
    """The Messenger activity, closed over the service that drafts (ENT-260).

    The doorbell workflow runs this between planning and sending, and treats
    every failure as "use the template". So the two error shapes matter less
    here than anywhere else on this queue: whatever comes back, a notification
    still goes out. What they buy is a workflow history that says WHY the
    template was used, which is the difference between a deployment whose
    Messenger is off and one whose Messenger is broken.
    """

    @activity.defn(name=DRAFT_MESSAGE_ACTIVITY_NAME)
    def draft_message(
        request: intelligence_pb2.DraftMessageRequest,
    ) -> intelligence_pb2.DraftMessageResponse:
        try:
            return service.run_draft_message(request)
        except ConnectError as exc:
            if exc.code == Code.INVALID_ARGUMENT:
                # A malformed request, or a context with no severity. Neither
                # changes by waiting, and a doorbell must not sit in a retry
                # loop over the words it would have been nicer to use.
                raise ApplicationError(str(exc), type="bad-request", non_retryable=True) from exc
            raise ApplicationError(str(exc), type=exc.code.name) from exc

    return draft_message


async def run_worker(
    service: IntelligenceService,
    address: str,
    namespace: str,
    concurrency: int,
    stop: threading.Event,
) -> None:
    """Connect, retrying while the engine finishes starting, then poll until
    asked to stop."""
    client = None
    attempt = 0
    while client is None and not stop.is_set():
        attempt += 1
        try:
            client = await Client.connect(address, namespace=namespace)
        except Exception as exc:  # noqa: BLE001 (the engine is not up yet)
            logger.info("waiting for temporal at %s (attempt %d): %s", address, attempt, exc)
            await asyncio.sleep(2)
    if client is None:
        return

    worker = Worker(
        client,
        task_queue=TASK_QUEUE,
        activities=[
            make_activity(service),
            make_watch_activity(service),
            make_draft_message_activity(service),
        ],
        activity_executor=ThreadPoolExecutor(max_workers=concurrency),
        max_concurrent_activities=concurrency,
    )
    logger.info(
        "temporal worker started task_queue=%s namespace=%s concurrency=%d",
        TASK_QUEUE,
        namespace,
        concurrency,
    )
    async with worker:
        while not stop.is_set():
            await asyncio.sleep(1)
    logger.info("temporal worker stopped")


def start_in_background(
    service: IntelligenceService, address: str, namespace: str, concurrency: int
) -> threading.Event:
    """Run the worker on its own thread with its own event loop, beside the
    WSGI server. Returns the event that stops it."""
    stop = threading.Event()

    def main() -> None:
        try:
            asyncio.run(run_worker(service, address, namespace, concurrency, stop))
        except Exception:  # noqa: BLE001
            logger.exception("the temporal worker stopped with an error; the RPC half keeps serving")

    thread = threading.Thread(target=main, name="temporal-worker", daemon=True)
    thread.start()
    return stop
