import os
import time

import psycopg2
from qdrant_client import QdrantClient

from src.config import Config
from src.sources import SOURCES, get_sources_by_tier
from src.pipeline.scraper import Scraper
from src.pipeline.parser import Parser
from src.pipeline.chunker import Chunker
from src.pipeline.embedder import OpenAIEmbedder, CohereEmbedder
from src.pipeline.indexer import Indexer
from src.db.postgres import DB
from src.utils.hashing import get_content_hash, make_doc_id
from src.utils.logging import get_logger

log = get_logger(__name__)


def process_document(source_url: str, scrape_result: dict,
                     config: Config, scraper: Scraper, parser: Parser,
                     chunker: Chunker, embedders: list, indexer: Indexer, db: DB):
    """Process a single scraped document through the full pipeline."""
    content = scrape_result["markdown"]
    content_hash = get_content_hash(content)
    doc_id = make_doc_id(source_url)

    try:
        # check if any embedder's collection needs update
        needs_update = any(
            indexer.should_reprocess(source_url, content_hash, e.collection_name)
            for e in embedders
        )

        if not needs_update:
            log.info("skipping_unchanged", source_url=source_url)
            db.log_ingestion(doc_id, source_url, chunk_count=None,
                           content_hash=content_hash, status="skipped")
            return

        # parse and chunk
        elements = parser.parse_markdown(content, source_url)
        parents, children = chunker.chunk(elements, doc_id)

        if not children:
            log.warning("no_chunks", source_url=source_url)
            return

        # store parent chunks in PostgreSQL
        db.upsert_parent_chunks(parents)

        # embed and index in all providers' collections
        texts = [c["text"] for c in children]
        new_chunk_ids = {c["id"] for c in children}

        for embedder in embedders:
            vectors = embedder.embed(texts)
            indexer.upsert_chunks(
                children, vectors, embedder.collection_name,
                content_hash=content_hash,
                embedding_model=embedder.model,
                scraped_at=scrape_result["scraped_at"]
            )
            indexer.delete_orphans(doc_id, new_chunk_ids, embedder.collection_name)

        db.log_ingestion(doc_id, source_url, chunk_count=len(children),
                        content_hash=content_hash, status="success")

        log.info("document_processed",
                source_url=source_url,
                chunk_count=len(children),
                parent_count=len(parents))

    except Exception as e:
        log.error("document_failed", source_url=source_url, error=str(e))
        db.log_ingestion(doc_id, source_url, chunk_count=None,
                        content_hash=content_hash, status="failed",
                        error_message=str(e))
        db.add_to_dead_letter(source_url, str(e))


def run_processors():
    """Run processor profile ingestion mode."""
    from src.processors.indexer import (
        create_processor_collection,
        index_processors,
        get_processor_count,
        get_postgres_processor_count,
        PROCESSOR_COLLECTION,
    )
    from src.processors.profiles import PROCESSOR_PROFILES

    config = Config()
    config.validate_processors_required()

    log.info("processor_ingestion_started", profile_count=len(PROCESSOR_PROFILES))

    # Initialize clients
    qdrant_client = QdrantClient(
        host=config.qdrant_host,
        port=config.qdrant_port,
        api_key=config.qdrant_api_key or None
    )
    pg_conn = psycopg2.connect(config.postgres_dsn)
    pg_conn.autocommit = True

    # Initialize embedder (only OpenAI for processors)
    embedder = OpenAIEmbedder(
        config.openai_api_key,
        config.openai_embedding_model,
        config.openai_embedding_dims,
        config.embedding_batch_size
    )

    try:
        # Create Qdrant collection
        create_processor_collection(qdrant_client, config, recreate=True)

        # Index all processor profiles
        indexed_count = index_processors(
            qdrant=qdrant_client,
            pg_conn=pg_conn,
            embedder=embedder,
            config=config,
        )

        # Verify counts
        qdrant_count = get_processor_count(qdrant_client)
        postgres_count = get_postgres_processor_count(pg_conn)

        log.info("processor_ingestion_complete",
                 indexed=indexed_count,
                 qdrant_count=qdrant_count,
                 postgres_count=postgres_count)

    finally:
        pg_conn.close()


def run():
    config = Config()

    # Handle processors mode separately
    if config.mode == "processors":
        run_processors()
        return

    config.validate_required()

    scraper = Scraper(config)
    parser = Parser()
    chunker = Chunker(config)
    embedders = [
        OpenAIEmbedder(config.openai_api_key, config.openai_embedding_model,
                      config.openai_embedding_dims, config.embedding_batch_size),
        CohereEmbedder(config.cohere_api_key, config.cohere_embedding_model,
                      config.embedding_batch_size),
    ]
    indexer = Indexer(config.qdrant_host, config.qdrant_port, config.qdrant_api_key)
    db = DB(config.postgres_dsn)

    mode = config.mode
    log.info("ingestion_started", mode=mode)

    if mode == "incremental":
        sources = get_sources_by_tier(max_tier=3)
    elif mode == "full":
        sources = SOURCES
    elif mode == "single":
        # for debugging: MODE=single SINGLE_URL=https://...
        url = os.getenv("SINGLE_URL", "")
        assert url, "SINGLE_URL required for single mode"
        sources = [s for s in SOURCES if s.url == url]
    else:
        sources = get_sources_by_tier(max_tier=3)

    for source in sources:
        log.info("processing_source", name=source.name, url=source.url)
        scrape_results = scraper.scrape_url(source.url, source.crawl_depth)

        for result in scrape_results:
            process_document(
                result["url"], result, config,
                scraper, parser, chunker, embedders, indexer, db
            )

        time.sleep(1)  # courtesy delay between sources

    log.info("ingestion_complete", sources_processed=len(sources))


if __name__ == "__main__":
    run()
