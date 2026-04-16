import os
from pydantic import BaseModel, Field, field_validator, model_validator
from pydantic_settings import BaseSettings
from typing import Literal


class Config(BaseSettings):
    """Configuration for the ingestion pipeline with validation."""

    # Mode
    mode: Literal["incremental", "full", "single"] = Field(
        default="incremental",
        description="Ingestion mode: incremental (daily), full (reconciliation), or single (debug)"
    )

    # AI providers
    openai_api_key: str = Field(default="", description="OpenAI API key for embeddings")
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

    # Embedding
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

    def validate_required(self) -> None:
        """Validate that required API keys and connection strings are set."""
        errors = []

        if not self.openai_api_key:
            errors.append("OPENAI_API_KEY is required")
        if not self.firecrawl_api_key:
            errors.append("FIRECRAWL_API_KEY is required")
        if not self.postgres_dsn:
            errors.append("POSTGRES_DSN is required")

        if errors:
            raise ValueError(f"Configuration validation failed: {'; '.join(errors)}")
