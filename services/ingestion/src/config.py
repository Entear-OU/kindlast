import os
from pydantic import BaseModel, Field, field_validator, model_validator
from pydantic_settings import BaseSettings
from typing import Literal


class Config(BaseSettings):
    """Configuration for the ingestion pipeline with validation."""

    # Mode
    mode: Literal["incremental", "full", "single", "processors"] = Field(
        default="incremental",
        description="Ingestion mode: incremental (daily), full (reconciliation), single (debug), or processors (seed processor profiles)"
    )

    # AI providers
    embedding_provider: Literal["openai", "cohere", "local"] = Field(
        default="openai",
        description="Embedding provider: openai, cohere, or local"
    )
    openai_api_key: str = Field(default="", description="OpenAI API key for embeddings")
    # Support both EMBEDDING_BASE_URL (preferred) and OPENAI_API_BASE_URL (legacy)
    embedding_base_url: str = Field(default="", description="Embedding API base URL (for local models like LMstudio)")
    openai_api_base_url: str = Field(default="", description="(Deprecated) Use EMBEDDING_BASE_URL instead")
    cohere_api_key: str = Field(default="", description="Cohere API key for embeddings")
    firecrawl_api_key: str = Field(default="", description="Firecrawl API key for web scraping")

    # Infrastructure
    qdrant_host: str = Field(default="localhost", description="Qdrant vector database host")
    qdrant_port: int = Field(default=6333, ge=1, le=65535, description="Qdrant port")
    qdrant_api_key: str = Field(default="", description="Qdrant API key (optional)")
    postgres_dsn: str = Field(default="", description="PostgreSQL connection string")

    # Collections
    openai_collection: str = Field(
        default="kindlast_openai_prod",
        description="Qdrant collection for OpenAI embeddings"
    )
    cohere_collection: str = Field(
        default="kindlast_cohere_prod",
        description="Qdrant collection for Cohere embeddings"
    )

    # Chunking
    max_chunk_chars: int = Field(
        default=1000, ge=100, le=5000,
        description="Child chunk size in characters"
    )
    max_parent_chars: int = Field(
        default=3000, ge=500, le=10000,
        description="Parent chunk size in characters"
    )
    chunk_overlap: int = Field(
        default=100, ge=0, le=500,
        description="Overlap between chunks"
    )

    # Embedding - generic settings (preferred for local provider)
    embedding_model: str = Field(
        default="",
        description="Embedding model name (used when EMBEDDING_PROVIDER=local)"
    )
    embedding_dimension: int = Field(
        default=0,
        description="Embedding dimension (used when EMBEDDING_PROVIDER=local)"
    )

    # Embedding - provider-specific settings (fallbacks)
    openai_embedding_model: str = Field(
        default="text-embedding-3-large",
        description="OpenAI embedding model"
    )
    openai_embedding_dims: int = Field(
        default=3072, ge=256, le=4096,
        description="OpenAI embedding dimensions"
    )
    cohere_embedding_model: str = Field(
        default="embed-multilingual-v3.0",
        description="Cohere embedding model"
    )
    cohere_embedding_dims: int = Field(
        default=1024, ge=256, le=2048,
        description="Cohere embedding dimensions"
    )
    embedding_batch_size: int = Field(
        default=100, ge=1, le=500,
        description="Batch size for embedding API calls"
    )

    # Rate limiting
    firecrawl_delay_ms: int = Field(
        default=500, ge=0, le=10000,
        description="Delay between Firecrawl requests in ms"
    )
    openai_rpm_limit: int = Field(
        default=3000, ge=1,
        description="OpenAI requests per minute limit"
    )

    model_config = {
        "env_prefix": "",
        "case_sensitive": False,
        "extra": "ignore",
    }

    @model_validator(mode="after")
    def validate_chunk_sizes(self) -> "Config":
        """Ensure parent chunk size is larger than child chunk size."""
        if self.max_parent_chars <= self.max_chunk_chars:
            raise ValueError(
                f"max_parent_chars ({self.max_parent_chars}) must be greater than "
                f"max_chunk_chars ({self.max_chunk_chars})"
            )
        if self.chunk_overlap >= self.max_chunk_chars:
            raise ValueError(
                f"chunk_overlap ({self.chunk_overlap}) must be less than "
                f"max_chunk_chars ({self.max_chunk_chars})"
            )
        return self

    @model_validator(mode="after")
    def resolve_embedding_config(self) -> "Config":
        """Resolve embedding configuration with generic vars taking precedence."""
        # Resolve base_url: EMBEDDING_BASE_URL takes precedence over OPENAI_API_BASE_URL
        if self.embedding_base_url:
            self.openai_api_base_url = self.embedding_base_url

        # For local provider, use generic EMBEDDING_MODEL and EMBEDDING_DIMENSION
        if self.embedding_provider == "local":
            if self.embedding_model:
                self.openai_embedding_model = self.embedding_model
            if self.embedding_dimension > 0:
                self.openai_embedding_dims = self.embedding_dimension

        return self

    def validate_required(self) -> None:
        """Validate that required API keys and connection strings are set."""
        errors = []

        # Validate embedding provider configuration
        if self.embedding_provider == "local":
            if not self.embedding_base_url and not self.openai_api_base_url:
                errors.append("EMBEDDING_BASE_URL is required when using local embedding provider")
        elif self.embedding_provider == "openai":
            if not self.openai_api_key:
                errors.append("OPENAI_API_KEY is required when using openai embedding provider")
        elif self.embedding_provider == "cohere":
            if not self.cohere_api_key:
                errors.append("COHERE_API_KEY is required when using cohere embedding provider")

        if not self.firecrawl_api_key:
            errors.append("FIRECRAWL_API_KEY is required")
        if not self.postgres_dsn:
            errors.append("POSTGRES_DSN is required")

        if errors:
            raise ValueError(f"Configuration validation failed: {'; '.join(errors)}")

    def validate_processors_required(self) -> None:
        """Validate configuration for processor profile ingestion mode."""
        errors = []

        # Validate embedding provider for processors
        if self.embedding_provider == "local":
            if not self.embedding_base_url and not self.openai_api_base_url:
                errors.append("EMBEDDING_BASE_URL is required for local processor embeddings")
        elif self.embedding_provider == "openai":
            if not self.openai_api_key:
                errors.append("OPENAI_API_KEY is required for processor embeddings")
        elif self.embedding_provider == "cohere":
            if not self.cohere_api_key:
                errors.append("COHERE_API_KEY is required for processor embeddings")

        if not self.postgres_dsn:
            errors.append("POSTGRES_DSN is required for processor storage")

        if errors:
            raise ValueError(f"Processor configuration validation failed: {'; '.join(errors)}")
