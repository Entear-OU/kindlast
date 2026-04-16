"""Tests for indexer module."""
import pytest
from unittest.mock import Mock, patch, MagicMock


class TestIndexer:
    """Tests for Indexer class."""

    @pytest.fixture
    def mock_qdrant_client(self):
        """Create a mock Qdrant client."""
        with patch("src.pipeline.indexer.QdrantClient") as mock:
            yield mock.return_value

    @pytest.fixture
    def indexer(self, mock_qdrant_client):
        """Create an Indexer instance with mocked client."""
        from src.pipeline.indexer import Indexer
        return Indexer(qdrant_host="localhost", qdrant_port=6333)

    def test_should_reprocess_new_document(self, indexer, mock_qdrant_client):
        """Test that new documents should be processed."""
        # Simulate no existing document
        mock_qdrant_client.scroll.return_value = ([], None)

        result = indexer.should_reprocess(
            source_url="https://example.com/new",
            new_hash="abc123",
            collection="test_collection"
        )

        assert result is True

    def test_should_reprocess_changed_document(self, indexer, mock_qdrant_client):
        """Test that changed documents should be processed."""
        # Simulate existing document with different hash
        mock_point = Mock()
        mock_point.payload = {"content_hash": "old_hash"}
        mock_qdrant_client.scroll.return_value = ([mock_point], None)

        result = indexer.should_reprocess(
            source_url="https://example.com/existing",
            new_hash="new_hash",
            collection="test_collection"
        )

        assert result is True

    def test_should_not_reprocess_unchanged_document(self, indexer, mock_qdrant_client):
        """Test that unchanged documents should be skipped."""
        # Simulate existing document with same hash
        mock_point = Mock()
        mock_point.payload = {"content_hash": "same_hash"}
        mock_qdrant_client.scroll.return_value = ([mock_point], None)

        result = indexer.should_reprocess(
            source_url="https://example.com/existing",
            new_hash="same_hash",
            collection="test_collection"
        )

        assert result is False

    def test_upsert_chunks(self, indexer, mock_qdrant_client):
        """Test upserting chunks to Qdrant."""
        chunks = [
            {
                "id": "550e8400-e29b-41d4-a716-446655440000",
                "text": "Test chunk text",
                "source_url": "https://example.com",
                "doc_id": "doc-123",
                "parent_id": "parent-1",
                "chunk_index": 0,
                "section_title": "Introduction",
                "is_table": False,
            }
        ]
        vectors = [[0.1] * 3072]

        indexer.upsert_chunks(
            child_chunks=chunks,
            vectors=vectors,
            collection="test_collection",
            content_hash="abc123",
            embedding_model="text-embedding-3-large",
            scraped_at="2024-01-01T00:00:00Z"
        )

        mock_qdrant_client.upsert.assert_called_once()
        call_args = mock_qdrant_client.upsert.call_args
        assert call_args.kwargs["collection_name"] == "test_collection"
        assert len(call_args.kwargs["points"]) == 1

    def test_upsert_chunks_batching(self, indexer, mock_qdrant_client):
        """Test that upserts are batched in groups of 100."""
        # Create 250 chunks
        chunks = [
            {
                "id": f"550e8400-e29b-41d4-a716-4466554400{i:02d}",
                "text": f"Test chunk {i}",
                "source_url": "https://example.com",
                "doc_id": "doc-123",
                "parent_id": "parent-1",
                "chunk_index": i,
                "section_title": "Introduction",
                "is_table": False,
            }
            for i in range(250)
        ]
        vectors = [[0.1] * 3072 for _ in range(250)]

        indexer.upsert_chunks(
            child_chunks=chunks,
            vectors=vectors,
            collection="test_collection",
            content_hash="abc123",
            embedding_model="text-embedding-3-large",
            scraped_at="2024-01-01T00:00:00Z"
        )

        # Should have 3 batches: 100 + 100 + 50
        assert mock_qdrant_client.upsert.call_count == 3

    def test_upsert_chunks_payload(self, indexer, mock_qdrant_client):
        """Test that upserted chunks have correct payload structure."""
        from src.pipeline.indexer import PointStruct

        chunks = [
            {
                "id": "550e8400-e29b-41d4-a716-446655440000",
                "text": "Test chunk text",
                "source_url": "https://example.com/test",
                "doc_id": "doc-123",
                "parent_id": "parent-1",
                "chunk_index": 5,
                "section_title": "Test Section",
                "is_table": True,
            }
        ]
        vectors = [[0.1] * 10]

        indexer.upsert_chunks(
            child_chunks=chunks,
            vectors=vectors,
            collection="test_collection",
            content_hash="abc123",
            embedding_model="test-model",
            scraped_at="2024-01-01T00:00:00Z"
        )

        call_args = mock_qdrant_client.upsert.call_args
        points = call_args.kwargs["points"]
        payload = points[0].payload

        assert payload["text"] == "Test chunk text"
        assert payload["source_url"] == "https://example.com/test"
        assert payload["doc_id"] == "doc-123"
        assert payload["parent_id"] == "parent-1"
        assert payload["chunk_index"] == 5
        assert payload["section_title"] == "Test Section"
        assert payload["is_table"] is True
        assert payload["content_hash"] == "abc123"
        assert payload["embedding_model"] == "test-model"
        assert payload["scraped_at"] == "2024-01-01T00:00:00Z"

    def test_delete_orphans_no_existing(self, indexer, mock_qdrant_client):
        """Test orphan cleanup when no existing chunks."""
        mock_qdrant_client.scroll.return_value = ([], None)

        indexer.delete_orphans(
            doc_id="doc-123",
            new_chunk_ids={"chunk-1", "chunk-2"},
            collection="test_collection"
        )

        # Should not call delete if no orphans
        mock_qdrant_client.delete.assert_not_called()

    def test_delete_orphans_with_orphans(self, indexer, mock_qdrant_client):
        """Test orphan cleanup when orphans exist."""
        # Simulate existing chunks
        mock_point1 = Mock()
        mock_point1.id = "chunk-1"
        mock_point2 = Mock()
        mock_point2.id = "chunk-orphan"  # This should be deleted

        mock_qdrant_client.scroll.return_value = ([mock_point1, mock_point2], None)

        indexer.delete_orphans(
            doc_id="doc-123",
            new_chunk_ids={"chunk-1"},  # chunk-orphan is not in new set
            collection="test_collection"
        )

        # Should call delete to remove orphans
        mock_qdrant_client.delete.assert_called_once()

    def test_init_with_api_key(self):
        """Test initialization with API key."""
        with patch("src.pipeline.indexer.QdrantClient") as mock_client_class:
            from src.pipeline.indexer import Indexer

            Indexer(
                qdrant_host="qdrant.example.com",
                qdrant_port=6333,
                api_key="test-api-key"
            )

            mock_client_class.assert_called_once_with(
                host="qdrant.example.com",
                port=6333,
                api_key="test-api-key"
            )

    def test_init_without_api_key(self):
        """Test initialization without API key."""
        with patch("src.pipeline.indexer.QdrantClient") as mock_client_class:
            from src.pipeline.indexer import Indexer

            Indexer(
                qdrant_host="localhost",
                qdrant_port=6333,
                api_key=""
            )

            mock_client_class.assert_called_once_with(
                host="localhost",
                port=6333,
                api_key=None
            )
