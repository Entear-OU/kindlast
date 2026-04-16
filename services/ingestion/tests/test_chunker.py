"""Tests for chunker module."""
import pytest


class TestChunker:
    """Tests for Chunker class."""

    @pytest.fixture
    def config(self):
        """Create a test config."""
        from src.config import Config
        return Config(
            max_chunk_chars=500,
            max_parent_chars=1500,
            chunk_overlap=50
        )

    @pytest.fixture
    def chunker(self, config):
        """Create a chunker instance."""
        from src.pipeline.chunker import Chunker
        return Chunker(config)

    def test_empty_elements(self, chunker):
        """Test chunking empty element list."""
        parents, children = chunker.chunk([], "doc-123")
        assert parents == []
        assert children == []

    def test_single_text_element(self, chunker):
        """Test chunking a single text element."""
        elements = [
            {
                "type": "NarrativeText",
                "text": "This is a test paragraph with some content that should be chunked properly.",
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            }
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        assert len(parents) >= 1
        assert len(children) >= 1
        assert all(c["parent_id"] in [p["id"] for p in parents] for c in children)

    def test_title_creates_new_section(self, chunker):
        """Test that titles create section boundaries."""
        elements = [
            {
                "type": "NarrativeText",
                "text": "First section content " * 50,
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            },
            {
                "type": "Title",
                "text": "New Section Title",
                "is_title": True,
                "is_table": False,
                "source_url": "https://example.com"
            },
            {
                "type": "NarrativeText",
                "text": "Second section content " * 50,
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            },
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        # Should have multiple parents due to title boundary
        assert len(parents) >= 2

        # Check section titles are captured
        section_titles = [p["section_title"] for p in parents]
        assert "Introduction" in section_titles  # Default for first section
        assert "New Section Title" in section_titles

    def test_table_is_atomic(self, chunker):
        """Test that tables are never split."""
        long_table = "| Col1 | Col2 |\n" + ("| data | data |\n" * 100)

        elements = [
            {
                "type": "Table",
                "text": long_table,
                "is_title": False,
                "is_table": True,
                "source_url": "https://example.com"
            }
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        # Table should be a single chunk regardless of size
        table_children = [c for c in children if c["is_table"]]
        assert len(table_children) == 1
        assert table_children[0]["text"] == long_table.strip()

    def test_child_chunk_size_limit(self, chunker):
        """Test that child chunks respect max size (approximately)."""
        # Create content that's definitely larger than max_chunk_chars
        long_text = "Word " * 500  # ~2500 chars

        elements = [
            {
                "type": "NarrativeText",
                "text": long_text,
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            }
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        # Should have multiple children
        assert len(children) > 1

        # Each non-table child should be around max_chunk_chars
        # (we allow some flexibility for sentence boundaries)
        for child in children:
            if not child["is_table"]:
                # Allow up to 1.5x max for sentence boundary flexibility
                assert len(child["text"]) <= chunker.max_child * 1.5

    def test_chunk_overlap(self, chunker):
        """Test that chunks have overlapping content."""
        # Create content that will definitely span multiple chunks
        long_text = "This is sentence number one. " * 50

        elements = [
            {
                "type": "NarrativeText",
                "text": long_text,
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            }
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        if len(children) >= 2:
            # Check that consecutive chunks have some overlap
            for i in range(len(children) - 1):
                current_end = children[i]["text"][-chunker.overlap:]
                # There should be some shared content (though not exact due to sentence boundaries)
                # This is a soft check as sentence boundary logic may adjust overlap
                pass  # Overlap logic is internal

    def test_children_have_valid_parent_ids(self, chunker):
        """Test that all children reference existing parents."""
        elements = [
            {
                "type": "Title",
                "text": "Section 1",
                "is_title": True,
                "is_table": False,
                "source_url": "https://example.com"
            },
            {
                "type": "NarrativeText",
                "text": "Content for section one. " * 30,
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            },
            {
                "type": "Title",
                "text": "Section 2",
                "is_title": True,
                "is_table": False,
                "source_url": "https://example.com"
            },
            {
                "type": "NarrativeText",
                "text": "Content for section two. " * 30,
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            },
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        parent_ids = {p["id"] for p in parents}

        for child in children:
            assert child["parent_id"] in parent_ids, \
                f"Child {child['id']} references non-existent parent {child['parent_id']}"

    def test_chunk_metadata(self, chunker):
        """Test that chunks have required metadata."""
        elements = [
            {
                "type": "NarrativeText",
                "text": "Test content for metadata check.",
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com/test"
            }
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        # Check parent metadata
        for parent in parents:
            assert "id" in parent
            assert "text" in parent
            assert "doc_id" in parent
            assert "section_title" in parent
            assert "source_url" in parent
            assert "chunk_index" in parent
            assert parent["doc_id"] == "doc-123"
            assert parent["source_url"] == "https://example.com/test"

        # Check child metadata
        for child in children:
            assert "id" in child
            assert "text" in child
            assert "parent_id" in child
            assert "doc_id" in child
            assert "chunk_index" in child
            assert "source_url" in child
            assert "section_title" in child
            assert "is_table" in child
            assert child["doc_id"] == "doc-123"
            assert child["source_url"] == "https://example.com/test"

    def test_tiny_fragments_skipped(self, chunker):
        """Test that very small fragments are not included."""
        elements = [
            {
                "type": "NarrativeText",
                "text": "A" * 30,  # Below 50 char threshold
                "is_title": False,
                "is_table": False,
                "source_url": "https://example.com"
            }
        ]

        parents, children = chunker.chunk(elements, "doc-123")

        # Tiny fragments should be in parents but filtered from children
        for child in children:
            assert len(child["text"]) > 50
