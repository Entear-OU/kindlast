"""
Indexes processor profiles into both PostgreSQL (structured lookup)
and Qdrant (semantic search for fuzzy matching).

When a DPO says "we use Stripe for payments", the RAG service needs to:
1. Exact-match "stripe" via processor_profiles table
2. Fuzzy-match "payment tool" via Qdrant processor collection
"""

import json
from typing import Optional

import psycopg2
from psycopg2.extras import execute_values
from qdrant_client import QdrantClient
from qdrant_client.models import (
    Distance,
    PointStruct,
    VectorParams,
)

from src.config import Config
from src.pipeline.embedder import EmbeddingProvider
from src.utils.logging import get_logger
from .profiles import PROCESSOR_PROFILES, ProcessorProfile

log = get_logger(__name__)

PROCESSOR_COLLECTION = "kindlast_processors"


def create_processor_collection(
    client: QdrantClient,
    config: Config,
    recreate: bool = True
) -> None:
    """
    Create Qdrant collection for processor profile embeddings.

    Args:
        client: Qdrant client instance
        config: Configuration with embedding dimensions
        recreate: If True, delete and recreate collection. If False, create only if missing.
    """
    collections = client.get_collections().collections
    collection_names = [c.name for c in collections]

    if PROCESSOR_COLLECTION in collection_names:
        if recreate:
            log.info("recreating_processor_collection", collection=PROCESSOR_COLLECTION)
            client.delete_collection(PROCESSOR_COLLECTION)
        else:
            log.info("processor_collection_exists", collection=PROCESSOR_COLLECTION)
            return

    client.create_collection(
        collection_name=PROCESSOR_COLLECTION,
        vectors_config=VectorParams(
            size=config.openai_embedding_dims,
            distance=Distance.COSINE,
        ),
    )
    log.info("processor_collection_created",
             collection=PROCESSOR_COLLECTION,
             dimensions=config.openai_embedding_dims)


def _build_embed_text(profile: ProcessorProfile) -> str:
    """
    Build text representation of a processor profile for embedding.

    This text is optimized for semantic search, containing all relevant
    information a DPO might query about.
    """
    data_cats = ", ".join(profile.data_categories) if profile.data_categories else "varies"
    purposes = ", ".join(profile.processing_purposes) if profile.processing_purposes else "varies"
    locations = ", ".join(profile.data_locations) if profile.data_locations else "global"

    return (
        f"{profile.name} ({profile.category}). "
        f"Processes: {data_cats}. "
        f"Purposes: {purposes}. "
        f"HQ: {profile.headquarters}. "
        f"Data locations: {locations}. "
        f"Transfer mechanism: {profile.transfer_mechanism}."
    )


def _ensure_processor_table(conn: psycopg2.extensions.connection) -> None:
    """Ensure processor_profiles table exists in PostgreSQL."""
    with conn.cursor() as cur:
        cur.execute("""
            CREATE TABLE IF NOT EXISTS processor_profiles (
                id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                name VARCHAR(255) NOT NULL UNIQUE,
                slug VARCHAR(100) NOT NULL UNIQUE,
                category VARCHAR(100),
                description TEXT,
                headquarters VARCHAR(2),
                data_categories JSONB DEFAULT '[]'::jsonb,
                processing_purposes JSONB DEFAULT '[]'::jsonb,
                data_locations JSONB DEFAULT '[]'::jsonb,
                transfer_mechanism VARCHAR(50),
                dpa_url TEXT,
                subprocessors_url TEXT,
                gdpr_page_url TEXT,
                verified BOOLEAN DEFAULT false,
                last_verified TIMESTAMPTZ,
                created_at TIMESTAMPTZ DEFAULT now(),
                updated_at TIMESTAMPTZ DEFAULT now()
            )
        """)
        cur.execute("""
            CREATE INDEX IF NOT EXISTS idx_processor_slug
            ON processor_profiles(slug)
        """)
        cur.execute("""
            CREATE INDEX IF NOT EXISTS idx_processor_category
            ON processor_profiles(category)
        """)
    conn.commit()
    log.info("processor_table_ensured")


def _upsert_processor_postgres(
    conn: psycopg2.extensions.connection,
    profile: ProcessorProfile
) -> None:
    """Upsert a single processor profile into PostgreSQL."""
    with conn.cursor() as cur:
        cur.execute("""
            INSERT INTO processor_profiles
                (name, slug, category, headquarters, data_categories,
                 processing_purposes, data_locations, transfer_mechanism,
                 dpa_url, subprocessors_url, gdpr_page_url, verified)
            VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, true)
            ON CONFLICT (slug) DO UPDATE SET
                name = EXCLUDED.name,
                category = EXCLUDED.category,
                headquarters = EXCLUDED.headquarters,
                data_categories = EXCLUDED.data_categories,
                processing_purposes = EXCLUDED.processing_purposes,
                data_locations = EXCLUDED.data_locations,
                transfer_mechanism = EXCLUDED.transfer_mechanism,
                dpa_url = EXCLUDED.dpa_url,
                subprocessors_url = EXCLUDED.subprocessors_url,
                gdpr_page_url = EXCLUDED.gdpr_page_url,
                verified = true,
                updated_at = now()
        """, (
            profile.name,
            profile.slug,
            profile.category,
            profile.headquarters,
            json.dumps(profile.data_categories),
            json.dumps(profile.processing_purposes),
            json.dumps(profile.data_locations),
            profile.transfer_mechanism,
            profile.dpa_url,
            profile.subprocessors_url,
            profile.gdpr_page_url,
        ))


def _upsert_processor_qdrant(
    client: QdrantClient,
    profile: ProcessorProfile,
    vector: list[float]
) -> None:
    """Upsert a single processor profile into Qdrant."""
    # Use a deterministic ID based on slug hash
    point_id = hash(profile.slug) & 0x7FFFFFFF  # Ensure positive 32-bit int

    client.upsert(
        collection_name=PROCESSOR_COLLECTION,
        points=[PointStruct(
            id=point_id,
            vector=vector,
            payload={
                "slug": profile.slug,
                "name": profile.name,
                "category": profile.category,
                "headquarters": profile.headquarters,
                "data_categories": profile.data_categories,
                "processing_purposes": profile.processing_purposes,
                "data_locations": profile.data_locations,
                "transfer_mechanism": profile.transfer_mechanism,
                "dpa_url": profile.dpa_url,
                "text": _build_embed_text(profile),
            },
        )],
    )


def index_processors(
    qdrant: QdrantClient,
    pg_conn: psycopg2.extensions.connection,
    embedder: EmbeddingProvider,
    config: Config,
    profiles: Optional[list[ProcessorProfile]] = None,
) -> int:
    """
    Index processor profiles into both PostgreSQL and Qdrant.

    Args:
        qdrant: Qdrant client instance
        pg_conn: PostgreSQL connection
        embedder: Embedding provider for creating vectors
        config: Configuration object
        profiles: Optional list of profiles to index. If None, uses PROCESSOR_PROFILES.

    Returns:
        Number of profiles indexed
    """
    if profiles is None:
        profiles = PROCESSOR_PROFILES

    if not profiles:
        log.warning("no_profiles_to_index")
        return 0

    # Ensure table exists
    _ensure_processor_table(pg_conn)

    # Build embed texts for all profiles
    embed_texts = [_build_embed_text(p) for p in profiles]

    # Embed all texts in batch
    log.info("embedding_profiles", count=len(profiles))
    vectors = embedder.embed(embed_texts)

    # Index each profile
    indexed_count = 0
    for profile, vector in zip(profiles, vectors):
        try:
            # 1. Upsert into PostgreSQL
            _upsert_processor_postgres(pg_conn, profile)

            # 2. Upsert into Qdrant
            _upsert_processor_qdrant(qdrant, profile, vector)

            indexed_count += 1
            log.debug("processor_indexed", slug=profile.slug, name=profile.name)

        except Exception as e:
            log.error("processor_index_failed",
                     slug=profile.slug,
                     error=str(e))
            # Continue with other profiles even if one fails

    pg_conn.commit()

    log.info("processors_indexed",
             total=len(profiles),
             indexed=indexed_count,
             failed=len(profiles) - indexed_count)

    return indexed_count


def search_processors(
    qdrant: QdrantClient,
    embedder: EmbeddingProvider,
    query: str,
    limit: int = 5
) -> list[dict]:
    """
    Search for processor profiles using semantic search.

    Args:
        qdrant: Qdrant client instance
        embedder: Embedding provider for query vectorization
        query: Search query (e.g., "payment processing tool", "crm system")
        limit: Maximum number of results to return

    Returns:
        List of matching processor profiles with scores
    """
    # Embed the query
    query_vector = embedder.embed([query])[0]

    # Search in Qdrant
    results = qdrant.search(
        collection_name=PROCESSOR_COLLECTION,
        query_vector=query_vector,
        limit=limit,
        with_payload=True
    )

    return [
        {
            "slug": hit.payload.get("slug"),
            "name": hit.payload.get("name"),
            "category": hit.payload.get("category"),
            "score": hit.score,
            "data_categories": hit.payload.get("data_categories", []),
            "transfer_mechanism": hit.payload.get("transfer_mechanism"),
        }
        for hit in results
    ]


def get_processor_count(qdrant: QdrantClient) -> int:
    """Get the number of indexed processors in Qdrant."""
    try:
        collection_info = qdrant.get_collection(PROCESSOR_COLLECTION)
        return collection_info.points_count
    except Exception:
        return 0


def get_postgres_processor_count(pg_conn: psycopg2.extensions.connection) -> int:
    """Get the number of processor profiles in PostgreSQL."""
    try:
        with pg_conn.cursor() as cur:
            cur.execute("SELECT COUNT(*) FROM processor_profiles")
            result = cur.fetchone()
            return result[0] if result else 0
    except Exception:
        return 0
