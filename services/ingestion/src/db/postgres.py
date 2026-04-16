import psycopg2
from psycopg2.extras import execute_values
from datetime import datetime
from typing import Optional
from src.utils.logging import get_logger

log = get_logger(__name__)


class DB:
    """PostgreSQL database handler for ingestion pipeline."""

    def __init__(self, dsn: str):
        self.dsn = dsn
        self.conn = None
        self._connect()
        self._ensure_tables()

    def _connect(self):
        """Establish database connection."""
        self.conn = psycopg2.connect(self.dsn)
        self.conn.autocommit = True

    def _ensure_tables(self):
        """Create tables if they don't exist."""
        with self.conn.cursor() as cur:
            # Ingestion log table
            cur.execute("""
                CREATE TABLE IF NOT EXISTS ingestion_log (
                    id SERIAL PRIMARY KEY,
                    doc_id VARCHAR(36) NOT NULL,
                    source_url TEXT NOT NULL,
                    chunk_count INTEGER,
                    content_hash VARCHAR(64) NOT NULL,
                    status VARCHAR(20) NOT NULL,
                    error_message TEXT,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    UNIQUE(doc_id)
                )
            """)

            # Parent chunks table
            cur.execute("""
                CREATE TABLE IF NOT EXISTS parent_chunks (
                    id VARCHAR(36) PRIMARY KEY,
                    doc_id VARCHAR(36) NOT NULL,
                    text TEXT NOT NULL,
                    section_title TEXT,
                    source_url TEXT NOT NULL,
                    chunk_index INTEGER NOT NULL,
                    is_table BOOLEAN DEFAULT FALSE,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
                )
            """)

            # Dead letter table for failed ingestions
            cur.execute("""
                CREATE TABLE IF NOT EXISTS ingestion_dead_letter (
                    id SERIAL PRIMARY KEY,
                    source_url TEXT NOT NULL,
                    error_message TEXT NOT NULL,
                    retry_count INTEGER DEFAULT 0,
                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                    last_retry_at TIMESTAMP
                )
            """)

            # Create indexes
            cur.execute("""
                CREATE INDEX IF NOT EXISTS idx_ingestion_log_source_url
                ON ingestion_log(source_url)
            """)
            cur.execute("""
                CREATE INDEX IF NOT EXISTS idx_parent_chunks_doc_id
                ON parent_chunks(doc_id)
            """)
            cur.execute("""
                CREATE INDEX IF NOT EXISTS idx_dead_letter_source_url
                ON ingestion_dead_letter(source_url)
            """)

        log.info("database_tables_ready")

    def log_ingestion(self, doc_id: str, source_url: str,
                      chunk_count: Optional[int], content_hash: str,
                      status: str, error_message: Optional[str] = None):
        """Log ingestion attempt to the ingestion_log table."""
        with self.conn.cursor() as cur:
            cur.execute("""
                INSERT INTO ingestion_log
                    (doc_id, source_url, chunk_count, content_hash, status, error_message, updated_at)
                VALUES (%s, %s, %s, %s, %s, %s, CURRENT_TIMESTAMP)
                ON CONFLICT (doc_id) DO UPDATE SET
                    chunk_count = EXCLUDED.chunk_count,
                    content_hash = EXCLUDED.content_hash,
                    status = EXCLUDED.status,
                    error_message = EXCLUDED.error_message,
                    updated_at = CURRENT_TIMESTAMP
            """, (doc_id, source_url, chunk_count, content_hash, status, error_message))

        log.info("ingestion_logged", doc_id=doc_id, status=status)

    def upsert_parent_chunks(self, parent_chunks: list[dict]):
        """Store parent chunks in PostgreSQL for context retrieval."""
        if not parent_chunks:
            return

        with self.conn.cursor() as cur:
            # Delete existing chunks for this doc_id
            doc_id = parent_chunks[0]["doc_id"]
            cur.execute("DELETE FROM parent_chunks WHERE doc_id = %s", (doc_id,))

            # Insert new chunks
            values = [
                (
                    chunk["id"],
                    chunk["doc_id"],
                    chunk["text"],
                    chunk["section_title"],
                    chunk["source_url"],
                    chunk["chunk_index"],
                    chunk.get("is_table", False)
                )
                for chunk in parent_chunks
            ]

            execute_values(cur, """
                INSERT INTO parent_chunks
                    (id, doc_id, text, section_title, source_url, chunk_index, is_table)
                VALUES %s
            """, values)

        log.info("parent_chunks_upserted", doc_id=doc_id, count=len(parent_chunks))

    def get_parent_chunk(self, parent_id: str) -> Optional[dict]:
        """Retrieve a parent chunk by ID."""
        with self.conn.cursor() as cur:
            cur.execute("""
                SELECT id, doc_id, text, section_title, source_url, chunk_index, is_table
                FROM parent_chunks WHERE id = %s
            """, (parent_id,))
            row = cur.fetchone()

            if row:
                return {
                    "id": row[0],
                    "doc_id": row[1],
                    "text": row[2],
                    "section_title": row[3],
                    "source_url": row[4],
                    "chunk_index": row[5],
                    "is_table": row[6]
                }
            return None

    def add_to_dead_letter(self, source_url: str, error_message: str):
        """Add failed document to dead letter queue for retry."""
        with self.conn.cursor() as cur:
            cur.execute("""
                INSERT INTO ingestion_dead_letter (source_url, error_message)
                VALUES (%s, %s)
                ON CONFLICT DO NOTHING
            """, (source_url, error_message))

        log.warning("added_to_dead_letter", source_url=source_url)

    def get_dead_letter_items(self, max_retries: int = 3) -> list[dict]:
        """Get items from dead letter queue that haven't exceeded max retries."""
        with self.conn.cursor() as cur:
            cur.execute("""
                SELECT id, source_url, error_message, retry_count
                FROM ingestion_dead_letter
                WHERE retry_count < %s
                ORDER BY created_at ASC
            """, (max_retries,))

            return [
                {
                    "id": row[0],
                    "source_url": row[1],
                    "error_message": row[2],
                    "retry_count": row[3]
                }
                for row in cur.fetchall()
            ]

    def mark_dead_letter_retried(self, item_id: int):
        """Increment retry count for a dead letter item."""
        with self.conn.cursor() as cur:
            cur.execute("""
                UPDATE ingestion_dead_letter
                SET retry_count = retry_count + 1, last_retry_at = CURRENT_TIMESTAMP
                WHERE id = %s
            """, (item_id,))

    def remove_from_dead_letter(self, source_url: str):
        """Remove successfully processed item from dead letter."""
        with self.conn.cursor() as cur:
            cur.execute(
                "DELETE FROM ingestion_dead_letter WHERE source_url = %s",
                (source_url,)
            )

    def close(self):
        """Close database connection."""
        if self.conn:
            self.conn.close()
