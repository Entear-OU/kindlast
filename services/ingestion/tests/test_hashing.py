"""Tests for hashing utilities."""
import pytest


class TestContentHash:
    """Tests for get_content_hash function."""

    def test_deterministic_hash(self):
        """Test that same content produces same hash."""
        from src.utils.hashing import get_content_hash

        content = "This is test content."
        hash1 = get_content_hash(content)
        hash2 = get_content_hash(content)

        assert hash1 == hash2

    def test_different_content_different_hash(self):
        """Test that different content produces different hash."""
        from src.utils.hashing import get_content_hash

        hash1 = get_content_hash("Content A")
        hash2 = get_content_hash("Content B")

        assert hash1 != hash2

    def test_hash_format(self):
        """Test that hash is a valid SHA-256 hex string."""
        from src.utils.hashing import get_content_hash

        hash_value = get_content_hash("test")

        # SHA-256 produces 64 hex characters
        assert len(hash_value) == 64
        assert all(c in "0123456789abcdef" for c in hash_value)

    def test_empty_content(self):
        """Test hashing empty content."""
        from src.utils.hashing import get_content_hash

        hash_value = get_content_hash("")

        # Should still produce valid hash
        assert len(hash_value) == 64

    def test_unicode_content(self):
        """Test hashing unicode content."""
        from src.utils.hashing import get_content_hash

        hash_value = get_content_hash("Test with unicode: 日本語 émojis 🎉")

        assert len(hash_value) == 64


class TestMakeDocId:
    """Tests for make_doc_id function."""

    def test_deterministic_doc_id(self):
        """Test that same URL produces same doc_id."""
        from src.utils.hashing import make_doc_id

        url = "https://example.com/test"
        id1 = make_doc_id(url)
        id2 = make_doc_id(url)

        assert id1 == id2

    def test_different_urls_different_ids(self):
        """Test that different URLs produce different doc_ids."""
        from src.utils.hashing import make_doc_id

        id1 = make_doc_id("https://example.com/page1")
        id2 = make_doc_id("https://example.com/page2")

        assert id1 != id2

    def test_valid_uuid_format(self):
        """Test that doc_id is a valid UUID string."""
        from src.utils.hashing import make_doc_id
        import uuid

        doc_id = make_doc_id("https://example.com")

        # Should be parseable as UUID
        parsed = uuid.UUID(doc_id)
        assert str(parsed) == doc_id


class TestMakeChunkId:
    """Tests for make_chunk_id function."""

    def test_deterministic_chunk_id(self):
        """Test that same inputs produce same chunk_id."""
        from src.utils.hashing import make_chunk_id

        id1 = make_chunk_id("doc-123", 0, "child")
        id2 = make_chunk_id("doc-123", 0, "child")

        assert id1 == id2

    def test_different_indices_different_ids(self):
        """Test that different indices produce different chunk_ids."""
        from src.utils.hashing import make_chunk_id

        id1 = make_chunk_id("doc-123", 0, "child")
        id2 = make_chunk_id("doc-123", 1, "child")

        assert id1 != id2

    def test_different_prefixes_different_ids(self):
        """Test that different prefixes produce different chunk_ids."""
        from src.utils.hashing import make_chunk_id

        id1 = make_chunk_id("doc-123", 0, "child")
        id2 = make_chunk_id("doc-123", 0, "parent")

        assert id1 != id2

    def test_different_doc_ids_different_chunk_ids(self):
        """Test that different doc_ids produce different chunk_ids."""
        from src.utils.hashing import make_chunk_id

        id1 = make_chunk_id("doc-123", 0, "child")
        id2 = make_chunk_id("doc-456", 0, "child")

        assert id1 != id2

    def test_valid_uuid_format(self):
        """Test that chunk_id is a valid UUID string."""
        from src.utils.hashing import make_chunk_id
        import uuid

        chunk_id = make_chunk_id("doc-123", 5, "child")

        # Should be parseable as UUID
        parsed = uuid.UUID(chunk_id)
        assert str(parsed) == chunk_id

    def test_default_prefix(self):
        """Test default prefix value."""
        from src.utils.hashing import make_chunk_id

        # Default prefix should be "chunk"
        id1 = make_chunk_id("doc-123", 0)
        id2 = make_chunk_id("doc-123", 0, "chunk")

        assert id1 == id2
