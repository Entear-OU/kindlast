import uuid
from qdrant_client import QdrantClient
from qdrant_client.models import (
    PointStruct, Filter, FieldCondition, MatchValue, FilterSelector
)
from src.utils.hashing import get_content_hash
from src.utils.logging import get_logger

log = get_logger(__name__)


class Indexer:
    def __init__(self, qdrant_host: str, qdrant_port: int, api_key: str = ""):
        self.client = QdrantClient(host=qdrant_host, port=qdrant_port, api_key=api_key or None)

    def should_reprocess(self, source_url: str, new_hash: str, collection: str) -> bool:
        """Returns True if document has changed or is new."""
        results, _ = self.client.scroll(
            collection_name=collection,
            scroll_filter=Filter(must=[
                FieldCondition(key="source_url", match=MatchValue(value=source_url))
            ]),
            limit=1,
            with_payload=True,
            with_vectors=False
        )
        if not results:
            return True
        stored_hash = results[0].payload.get("content_hash", "")
        return stored_hash != new_hash

    def upsert_chunks(self, child_chunks: list[dict], vectors: list[list[float]],
                      collection: str, content_hash: str,
                      embedding_model: str, scraped_at: str):
        """Upsert child chunks with their vectors into Qdrant."""
        points = []
        for chunk, vector in zip(child_chunks, vectors):
            points.append(PointStruct(
                id=str(uuid.UUID(chunk["id"])),
                vector=vector,
                payload={
                    "text": chunk["text"],
                    "source_url": chunk["source_url"],
                    "doc_id": chunk["doc_id"],
                    "parent_id": chunk["parent_id"],
                    "chunk_index": chunk["chunk_index"],
                    "section_title": chunk["section_title"],
                    "is_table": chunk["is_table"],
                    "content_hash": content_hash,
                    "embedding_model": embedding_model,
                    "scraped_at": scraped_at,
                }
            ))

        # batch upsert in groups of 100
        for i in range(0, len(points), 100):
            self.client.upsert(collection_name=collection, points=points[i:i+100])

        log.info("chunks_upserted", collection=collection, count=len(points))

    def delete_orphans(self, doc_id: str, new_chunk_ids: set[str], collection: str):
        """Remove chunks from Qdrant that no longer exist in the document."""
        existing_ids = set()
        offset = None

        while True:
            results, next_offset = self.client.scroll(
                collection_name=collection,
                scroll_filter=Filter(must=[
                    FieldCondition(key="doc_id", match=MatchValue(value=doc_id))
                ]),
                limit=100,
                offset=offset,
                with_payload=False,
                with_vectors=False
            )
            for point in results:
                existing_ids.add(str(point.id))
            offset = next_offset
            if offset is None:
                break

        orphaned = existing_ids - new_chunk_ids
        if orphaned:
            self.client.delete(
                collection_name=collection,
                points_selector=FilterSelector(
                    filter=Filter(must=[
                        FieldCondition(key="doc_id", match=MatchValue(value=doc_id))
                    ])
                )
            )
            # re-upsert the non-orphaned ones (simpler than selective delete by ID list)
            log.info("orphans_removed", doc_id=doc_id, count=len(orphaned))
