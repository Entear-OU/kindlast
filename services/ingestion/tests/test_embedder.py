"""Tests for embedder module."""
import pytest
from unittest.mock import Mock, patch, MagicMock


class TestOpenAIEmbedder:
    """Tests for OpenAIEmbedder class."""

    def test_collection_name(self):
        """Test that collection name is correct."""
        from src.pipeline.embedder import OpenAIEmbedder

        with patch("src.pipeline.embedder.OpenAI"):
            embedder = OpenAIEmbedder(api_key="test-key")
            assert embedder.collection_name == "kindlast_openai_prod"

    def test_dimensions(self):
        """Test that dimensions are correct."""
        from src.pipeline.embedder import OpenAIEmbedder

        with patch("src.pipeline.embedder.OpenAI"):
            # Default dimensions
            embedder = OpenAIEmbedder(api_key="test-key")
            assert embedder.dimensions == 3072

            # Custom dimensions
            embedder = OpenAIEmbedder(api_key="test-key", dimensions=1536)
            assert embedder.dimensions == 1536

    def test_embed_single_text(self):
        """Test embedding a single text."""
        from src.pipeline.embedder import OpenAIEmbedder

        mock_client = Mock()
        mock_response = Mock()
        mock_response.data = [Mock(embedding=[0.1] * 3072)]
        mock_client.embeddings.create.return_value = mock_response

        with patch("src.pipeline.embedder.OpenAI", return_value=mock_client):
            embedder = OpenAIEmbedder(api_key="test-key")
            result = embedder.embed(["test text"])

            assert len(result) == 1
            assert len(result[0]) == 3072
            mock_client.embeddings.create.assert_called_once()

    def test_embed_batching(self):
        """Test that embeddings are batched correctly."""
        from src.pipeline.embedder import OpenAIEmbedder

        mock_client = Mock()

        def mock_embed_create(**kwargs):
            batch = kwargs["input"]
            mock_response = Mock()
            mock_response.data = [Mock(embedding=[0.1] * 3072) for _ in batch]
            return mock_response

        mock_client.embeddings.create.side_effect = mock_embed_create

        with patch("src.pipeline.embedder.OpenAI", return_value=mock_client):
            embedder = OpenAIEmbedder(api_key="test-key", batch_size=50)
            texts = [f"text {i}" for i in range(120)]  # 120 texts, batch_size=50

            result = embedder.embed(texts)

            assert len(result) == 120
            # Should have 3 batches: 50 + 50 + 20
            assert mock_client.embeddings.create.call_count == 3


class TestLocalEmbedder:
    """Tests for local embedder (LMStudio) via OpenAIEmbedder."""

    def test_local_embedder_uses_base_url(self):
        """Test that local embedder passes base_url to OpenAI client."""
        from src.pipeline.embedder import OpenAIEmbedder

        with patch("src.pipeline.embedder.OpenAI") as mock_openai:
            embedder = OpenAIEmbedder(
                api_key="lm-studio",
                model="text-embedding-nomic-embed-text-v1.5",
                dimensions=768,
                base_url="http://localhost:1234/v1",
                collection_name="kindlast_local_test"
            )

            # Verify OpenAI client was created with base_url
            mock_openai.assert_called_once_with(
                api_key="lm-studio",
                base_url="http://localhost:1234/v1"
            )
            assert embedder._is_local is True
            assert embedder.collection_name == "kindlast_local_test"

    def test_local_embedder_no_dimensions_param(self):
        """Test that local embedder doesn't pass dimensions to API."""
        from src.pipeline.embedder import OpenAIEmbedder

        mock_client = Mock()
        mock_response = Mock()
        mock_response.data = [Mock(embedding=[0.1] * 768)]
        mock_client.embeddings.create.return_value = mock_response

        with patch("src.pipeline.embedder.OpenAI", return_value=mock_client):
            embedder = OpenAIEmbedder(
                api_key="lm-studio",
                model="local-model",
                dimensions=768,
                base_url="http://localhost:1234/v1"
            )
            embedder.embed(["test text"])

            # Verify dimensions was NOT passed (local models don't support it)
            call_kwargs = mock_client.embeddings.create.call_args[1]
            assert "dimensions" not in call_kwargs

    def test_openai_embedder_passes_dimensions(self):
        """Test that non-local embedder passes dimensions to API."""
        from src.pipeline.embedder import OpenAIEmbedder

        mock_client = Mock()
        mock_response = Mock()
        mock_response.data = [Mock(embedding=[0.1] * 3072)]
        mock_client.embeddings.create.return_value = mock_response

        with patch("src.pipeline.embedder.OpenAI", return_value=mock_client):
            embedder = OpenAIEmbedder(api_key="test-key")  # No base_url = not local
            embedder.embed(["test text"])

            # Verify dimensions WAS passed
            call_kwargs = mock_client.embeddings.create.call_args[1]
            assert call_kwargs["dimensions"] == 3072


class TestCohereEmbedder:
    """Tests for CohereEmbedder class."""

    def test_collection_name(self):
        """Test that collection name is correct."""
        from src.pipeline.embedder import CohereEmbedder

        with patch("src.pipeline.embedder.cohere.Client"):
            embedder = CohereEmbedder(api_key="test-key")
            assert embedder.collection_name == "kindlast_cohere_prod"

    def test_dimensions(self):
        """Test that dimensions are correct."""
        from src.pipeline.embedder import CohereEmbedder

        with patch("src.pipeline.embedder.cohere.Client"):
            embedder = CohereEmbedder(api_key="test-key")
            assert embedder.dimensions == 1024

    def test_embed_single_text(self):
        """Test embedding a single text."""
        from src.pipeline.embedder import CohereEmbedder

        mock_client = Mock()
        mock_response = Mock()
        mock_response.embeddings = [[0.1] * 1024]
        mock_client.embed.return_value = mock_response

        with patch("src.pipeline.embedder.cohere.Client", return_value=mock_client):
            embedder = CohereEmbedder(api_key="test-key")
            result = embedder.embed(["test text"])

            assert len(result) == 1
            assert len(result[0]) == 1024
            mock_client.embed.assert_called_once()

    def test_embed_uses_search_document_type(self):
        """Test that Cohere uses search_document input type."""
        from src.pipeline.embedder import CohereEmbedder

        mock_client = Mock()
        mock_response = Mock()
        mock_response.embeddings = [[0.1] * 1024]
        mock_client.embed.return_value = mock_response

        with patch("src.pipeline.embedder.cohere.Client", return_value=mock_client):
            embedder = CohereEmbedder(api_key="test-key")
            embedder.embed(["test text"])

            mock_client.embed.assert_called_with(
                texts=["test text"],
                model="embed-multilingual-v3.0",
                input_type="search_document"
            )

    def test_embed_batching(self):
        """Test that embeddings are batched correctly."""
        from src.pipeline.embedder import CohereEmbedder

        mock_client = Mock()

        def mock_embed(**kwargs):
            batch = kwargs["texts"]
            mock_response = Mock()
            mock_response.embeddings = [[0.1] * 1024 for _ in batch]
            return mock_response

        mock_client.embed.side_effect = mock_embed

        with patch("src.pipeline.embedder.cohere.Client", return_value=mock_client):
            embedder = CohereEmbedder(api_key="test-key", batch_size=50)
            texts = [f"text {i}" for i in range(120)]  # 120 texts

            result = embedder.embed(texts)

            assert len(result) == 120
            # Should have 3 batches: 50 + 50 + 20
            assert mock_client.embed.call_count == 3


class TestEmbeddingProvider:
    """Tests for EmbeddingProvider interface."""

    def test_abstract_methods(self):
        """Test that EmbeddingProvider is abstract."""
        from src.pipeline.embedder import EmbeddingProvider

        # Cannot instantiate abstract class
        with pytest.raises(TypeError):
            EmbeddingProvider()

    def test_openai_implements_interface(self):
        """Test that OpenAIEmbedder implements all required methods."""
        from src.pipeline.embedder import OpenAIEmbedder, EmbeddingProvider

        with patch("src.pipeline.embedder.OpenAI"):
            embedder = OpenAIEmbedder(api_key="test")

            assert isinstance(embedder, EmbeddingProvider)
            assert hasattr(embedder, "embed")
            assert hasattr(embedder, "collection_name")
            assert hasattr(embedder, "dimensions")

    def test_cohere_implements_interface(self):
        """Test that CohereEmbedder implements all required methods."""
        from src.pipeline.embedder import CohereEmbedder, EmbeddingProvider

        with patch("src.pipeline.embedder.cohere.Client"):
            embedder = CohereEmbedder(api_key="test")

            assert isinstance(embedder, EmbeddingProvider)
            assert hasattr(embedder, "embed")
            assert hasattr(embedder, "collection_name")
            assert hasattr(embedder, "dimensions")
