"""Tests for configuration module."""
import os
import pytest
from pydantic import ValidationError


class TestConfig:
    """Tests for Config class."""

    def test_default_values(self):
        """Test that Config initializes with correct defaults."""
        # Clear environment variables that might interfere
        env_vars = [
            "MODE", "OPENAI_API_KEY", "COHERE_API_KEY", "FIRECRAWL_API_KEY",
            "QDRANT_HOST", "QDRANT_PORT", "QDRANT_API_KEY", "POSTGRES_DSN"
        ]
        original_values = {k: os.environ.pop(k, None) for k in env_vars}

        try:
            from src.config import Config
            config = Config()

            assert config.mode == "incremental"
            assert config.qdrant_host == "localhost"
            assert config.qdrant_port == 6333
            assert config.max_chunk_chars == 1000
            assert config.max_parent_chars == 3000
            assert config.chunk_overlap == 100
            assert config.openai_embedding_model == "text-embedding-3-large"
            assert config.openai_embedding_dims == 3072
            assert config.cohere_embedding_model == "embed-multilingual-v3.0"
            assert config.cohere_embedding_dims == 1024
            assert config.embedding_batch_size == 100
            assert config.firecrawl_delay_ms == 500
        finally:
            # Restore original environment variables
            for k, v in original_values.items():
                if v is not None:
                    os.environ[k] = v

    def test_mode_validation(self):
        """Test that mode only accepts valid values."""
        from src.config import Config

        # Valid modes should work
        for mode in ["incremental", "full", "single"]:
            config = Config(mode=mode)
            assert config.mode == mode

        # Invalid mode should fail
        with pytest.raises(ValidationError):
            Config(mode="invalid")

    def test_port_validation(self):
        """Test that port must be in valid range."""
        from src.config import Config

        # Valid port
        config = Config(qdrant_port=6333)
        assert config.qdrant_port == 6333

        # Port too low
        with pytest.raises(ValidationError):
            Config(qdrant_port=0)

        # Port too high
        with pytest.raises(ValidationError):
            Config(qdrant_port=70000)

    def test_chunk_size_validation(self):
        """Test that parent chunk must be larger than child chunk."""
        from src.config import Config

        # Valid: parent > child
        config = Config(max_chunk_chars=1000, max_parent_chars=3000)
        assert config.max_chunk_chars == 1000
        assert config.max_parent_chars == 3000

        # Invalid: parent <= child
        with pytest.raises(ValidationError):
            Config(max_chunk_chars=3000, max_parent_chars=2000)

        # Invalid: parent == child
        with pytest.raises(ValidationError):
            Config(max_chunk_chars=2000, max_parent_chars=2000)

    def test_overlap_validation(self):
        """Test that overlap must be smaller than chunk size."""
        from src.config import Config

        # Valid: overlap < chunk
        config = Config(max_chunk_chars=1000, chunk_overlap=100)
        assert config.chunk_overlap == 100

        # Invalid: overlap >= chunk
        with pytest.raises(ValidationError):
            Config(max_chunk_chars=1000, chunk_overlap=1000)

    def test_validate_required_missing_keys(self):
        """Test that validate_required raises error for missing API keys."""
        from src.config import Config

        config = Config()

        with pytest.raises(ValueError) as exc_info:
            config.validate_required()

        error_msg = str(exc_info.value)
        assert "OPENAI_API_KEY is required" in error_msg
        assert "FIRECRAWL_API_KEY is required" in error_msg
        assert "POSTGRES_DSN is required" in error_msg

    def test_validate_required_with_keys(self):
        """Test that validate_required passes when all keys are set."""
        from src.config import Config

        config = Config(
            openai_api_key="sk-test",
            firecrawl_api_key="fc-test",
            postgres_dsn="postgresql://localhost/test"
        )

        # Should not raise
        config.validate_required()

    def test_environment_variable_loading(self, monkeypatch):
        """Test that Config loads from environment variables."""
        from src.config import Config

        monkeypatch.setenv("MODE", "full")
        monkeypatch.setenv("QDRANT_HOST", "qdrant.example.com")
        monkeypatch.setenv("QDRANT_PORT", "6334")
        monkeypatch.setenv("OPENAI_API_KEY", "sk-from-env")

        config = Config()

        assert config.mode == "full"
        assert config.qdrant_host == "qdrant.example.com"
        assert config.qdrant_port == 6334
        assert config.openai_api_key == "sk-from-env"
