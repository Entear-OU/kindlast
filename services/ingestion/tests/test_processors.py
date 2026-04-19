"""Tests for processor profile ingestion module."""
import json
import pytest
from unittest.mock import Mock, patch, MagicMock
from dataclasses import asdict


class TestProcessorProfile:
    """Tests for ProcessorProfile dataclass."""

    def test_processor_profile_creation(self):
        """Test creating a ProcessorProfile with all fields."""
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="Test Service",
            slug="test-service",
            category="payment",
            headquarters="US",
            data_categories=["name", "email", "payment_card"],
            processing_purposes=["payment_processing", "fraud_detection"],
            data_locations=["us", "eu"],
            transfer_mechanism="dpf",
            dpa_url="https://example.com/dpa",
            subprocessors_url="https://example.com/subprocessors",
            gdpr_page_url="https://example.com/gdpr"
        )

        assert profile.name == "Test Service"
        assert profile.slug == "test-service"
        assert profile.category == "payment"
        assert profile.headquarters == "US"
        assert len(profile.data_categories) == 3
        assert profile.transfer_mechanism == "dpf"

    def test_processor_profile_defaults(self):
        """Test ProcessorProfile default values."""
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="Minimal Service",
            slug="minimal",
            category="other",
            headquarters="DE"
        )

        assert profile.data_categories == []
        assert profile.processing_purposes == []
        assert profile.data_locations == []
        assert profile.transfer_mechanism == "scc"
        assert profile.dpa_url is None
        assert profile.subprocessors_url is None
        assert profile.gdpr_page_url is None

    def test_processor_profile_to_dict(self):
        """Test converting ProcessorProfile to dictionary."""
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="Dict Test",
            slug="dict-test",
            category="analytics",
            headquarters="GB",
            data_categories=["user_id"],
            processing_purposes=["analytics"]
        )

        profile_dict = asdict(profile)

        assert profile_dict["name"] == "Dict Test"
        assert profile_dict["slug"] == "dict-test"
        assert isinstance(profile_dict["data_categories"], list)


class TestProcessorProfiles:
    """Tests for the PROCESSOR_PROFILES list."""

    def test_processor_profiles_not_empty(self):
        """Test that PROCESSOR_PROFILES contains entries."""
        from src.processors.profiles import PROCESSOR_PROFILES

        assert len(PROCESSOR_PROFILES) >= 20, "Should have at least 20 processor profiles"

    def test_processor_profiles_unique_slugs(self):
        """Test that all processor slugs are unique."""
        from src.processors.profiles import PROCESSOR_PROFILES

        slugs = [p.slug for p in PROCESSOR_PROFILES]
        assert len(slugs) == len(set(slugs)), "All slugs must be unique"

    def test_processor_profiles_unique_names(self):
        """Test that all processor names are unique."""
        from src.processors.profiles import PROCESSOR_PROFILES

        names = [p.name for p in PROCESSOR_PROFILES]
        assert len(names) == len(set(names)), "All names must be unique"

    def test_processor_profiles_valid_headquarters(self):
        """Test that all headquarters codes are valid ISO 3166-1 alpha-2."""
        from src.processors.profiles import PROCESSOR_PROFILES

        for profile in PROCESSOR_PROFILES:
            assert len(profile.headquarters) == 2, f"{profile.name} has invalid headquarters code"
            assert profile.headquarters.isupper(), f"{profile.name} headquarters should be uppercase"

    def test_processor_profiles_valid_transfer_mechanisms(self):
        """Test that all transfer mechanisms are valid."""
        from src.processors.profiles import PROCESSOR_PROFILES

        valid_mechanisms = {"scc", "dpf", "adequacy", "none_required"}

        for profile in PROCESSOR_PROFILES:
            assert profile.transfer_mechanism in valid_mechanisms, \
                f"{profile.name} has invalid transfer_mechanism: {profile.transfer_mechanism}"

    def test_processor_profiles_have_categories(self):
        """Test that all profiles have a category."""
        from src.processors.profiles import PROCESSOR_PROFILES

        for profile in PROCESSOR_PROFILES:
            assert profile.category, f"{profile.name} is missing category"

    def test_stripe_profile_exists(self):
        """Test that Stripe profile exists with expected data."""
        from src.processors.profiles import get_processor_by_slug

        stripe = get_processor_by_slug("stripe")

        assert stripe is not None
        assert stripe.name == "Stripe"
        assert stripe.category == "payment"
        assert stripe.headquarters == "US"
        assert "payment_card" in stripe.data_categories
        assert "payment_processing" in stripe.processing_purposes
        assert stripe.transfer_mechanism == "dpf"
        assert stripe.dpa_url is not None

    def test_hubspot_profile_exists(self):
        """Test that HubSpot profile exists with expected data."""
        from src.processors.profiles import get_processor_by_slug

        hubspot = get_processor_by_slug("hubspot")

        assert hubspot is not None
        assert hubspot.name == "HubSpot"
        assert hubspot.category == "crm"
        assert "email" in hubspot.data_categories
        assert "crm" in hubspot.processing_purposes

    def test_aws_profile_exists(self):
        """Test that AWS profile exists with expected data."""
        from src.processors.profiles import get_processor_by_slug

        aws = get_processor_by_slug("aws")

        assert aws is not None
        assert aws.name == "Amazon Web Services"
        assert aws.category == "cloud_infrastructure"
        assert "hosting" in aws.processing_purposes

    def test_google_workspace_profile_exists(self):
        """Test that Google Workspace profile exists."""
        from src.processors.profiles import get_processor_by_slug

        gw = get_processor_by_slug("google-workspace")

        assert gw is not None
        assert gw.name == "Google Workspace"
        assert gw.category == "productivity"

    def test_get_processor_by_slug_not_found(self):
        """Test that non-existent slug returns None."""
        from src.processors.profiles import get_processor_by_slug

        result = get_processor_by_slug("nonexistent-slug")
        assert result is None

    def test_get_processors_by_category(self):
        """Test filtering processors by category."""
        from src.processors.profiles import get_processors_by_category

        payment_processors = get_processors_by_category("payment")

        assert len(payment_processors) >= 3
        for p in payment_processors:
            assert p.category == "payment"

    def test_get_eu_based_processors(self):
        """Test getting EU-headquartered processors."""
        from src.processors.profiles import get_eu_based_processors

        eu_processors = get_eu_based_processors()

        assert len(eu_processors) >= 1
        eu_countries = {
            "AT", "BE", "BG", "HR", "CY", "CZ", "DK", "EE", "FI", "FR",
            "DE", "GR", "HU", "IE", "IT", "LV", "LT", "LU", "MT", "NL",
            "PL", "PT", "RO", "SK", "SI", "ES", "SE", "IS", "LI", "NO"
        }
        for p in eu_processors:
            assert p.headquarters in eu_countries, f"{p.name} is not EU-based"

    def test_get_processors_requiring_transfer_mechanism(self):
        """Test getting processors that require transfer mechanisms."""
        from src.processors.profiles import get_processors_requiring_transfer_mechanism

        transfer_processors = get_processors_requiring_transfer_mechanism()

        assert len(transfer_processors) >= 10
        for p in transfer_processors:
            assert p.transfer_mechanism in ("scc", "dpf")


class TestProcessorIndexer:
    """Tests for processor profile indexing functions."""

    @pytest.fixture
    def mock_qdrant_client(self):
        """Create a mock Qdrant client."""
        client = Mock()
        client.get_collections.return_value.collections = []
        return client

    @pytest.fixture
    def mock_pg_connection(self):
        """Create a mock PostgreSQL connection."""
        conn = Mock()
        cursor = MagicMock()
        conn.cursor.return_value.__enter__ = Mock(return_value=cursor)
        conn.cursor.return_value.__exit__ = Mock(return_value=False)
        return conn

    @pytest.fixture
    def mock_embedder(self):
        """Create a mock embedding provider."""
        embedder = Mock()
        # Return a list of vectors matching the number of input texts
        embedder.embed = Mock(side_effect=lambda texts: [[0.1] * 3072 for _ in texts])
        return embedder

    @pytest.fixture
    def mock_config(self):
        """Create a mock configuration."""
        config = Mock()
        config.openai_embedding_dims = 3072
        return config

    def test_build_embed_text(self):
        """Test building embed text for a processor profile."""
        from src.processors.indexer import _build_embed_text
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="Test Service",
            slug="test-service",
            category="payment",
            headquarters="US",
            data_categories=["name", "email"],
            processing_purposes=["payment_processing"],
            data_locations=["us", "eu"],
            transfer_mechanism="dpf"
        )

        text = _build_embed_text(profile)

        assert "Test Service" in text
        assert "payment" in text
        assert "name, email" in text
        assert "payment_processing" in text
        assert "US" in text
        assert "dpf" in text

    def test_create_processor_collection(
        self, mock_qdrant_client, mock_config
    ):
        """Test creating the processor collection in Qdrant."""
        from src.processors.indexer import create_processor_collection, PROCESSOR_COLLECTION

        create_processor_collection(mock_qdrant_client, mock_config, recreate=True)

        mock_qdrant_client.create_collection.assert_called_once()
        call_kwargs = mock_qdrant_client.create_collection.call_args.kwargs
        assert call_kwargs["collection_name"] == PROCESSOR_COLLECTION

    def test_create_processor_collection_exists_no_recreate(
        self, mock_qdrant_client, mock_config
    ):
        """Test that existing collection is not recreated when recreate=False."""
        from src.processors.indexer import create_processor_collection, PROCESSOR_COLLECTION

        # Simulate existing collection
        existing_collection = Mock()
        existing_collection.name = PROCESSOR_COLLECTION
        mock_qdrant_client.get_collections.return_value.collections = [existing_collection]

        create_processor_collection(mock_qdrant_client, mock_config, recreate=False)

        mock_qdrant_client.delete_collection.assert_not_called()
        mock_qdrant_client.create_collection.assert_not_called()

    def test_create_processor_collection_exists_recreate(
        self, mock_qdrant_client, mock_config
    ):
        """Test that existing collection is deleted and recreated when recreate=True."""
        from src.processors.indexer import create_processor_collection, PROCESSOR_COLLECTION

        # Simulate existing collection
        existing_collection = Mock()
        existing_collection.name = PROCESSOR_COLLECTION
        mock_qdrant_client.get_collections.return_value.collections = [existing_collection]

        create_processor_collection(mock_qdrant_client, mock_config, recreate=True)

        mock_qdrant_client.delete_collection.assert_called_once_with(PROCESSOR_COLLECTION)
        mock_qdrant_client.create_collection.assert_called_once()

    def test_index_processors_empty_list(
        self, mock_qdrant_client, mock_pg_connection, mock_embedder, mock_config
    ):
        """Test indexing with empty profile list."""
        from src.processors.indexer import index_processors

        result = index_processors(
            qdrant=mock_qdrant_client,
            pg_conn=mock_pg_connection,
            embedder=mock_embedder,
            config=mock_config,
            profiles=[]
        )

        assert result == 0
        mock_embedder.embed.assert_not_called()

    def test_index_processors_single_profile(
        self, mock_qdrant_client, mock_pg_connection, mock_embedder, mock_config
    ):
        """Test indexing a single processor profile."""
        from src.processors.indexer import index_processors
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="Test Service",
            slug="test-service",
            category="payment",
            headquarters="US",
            data_categories=["email"],
            processing_purposes=["testing"],
            transfer_mechanism="dpf"
        )

        result = index_processors(
            qdrant=mock_qdrant_client,
            pg_conn=mock_pg_connection,
            embedder=mock_embedder,
            config=mock_config,
            profiles=[profile]
        )

        assert result == 1
        mock_embedder.embed.assert_called_once()
        mock_qdrant_client.upsert.assert_called_once()
        mock_pg_connection.commit.assert_called()

    def test_index_processors_uses_all_profiles_by_default(
        self, mock_qdrant_client, mock_pg_connection, mock_embedder, mock_config
    ):
        """Test that index_processors uses PROCESSOR_PROFILES when profiles=None."""
        from src.processors.indexer import index_processors
        from src.processors.profiles import PROCESSOR_PROFILES

        result = index_processors(
            qdrant=mock_qdrant_client,
            pg_conn=mock_pg_connection,
            embedder=mock_embedder,
            config=mock_config,
            profiles=None
        )

        assert result == len(PROCESSOR_PROFILES)

    def test_index_processors_postgres_upsert(
        self, mock_qdrant_client, mock_pg_connection, mock_embedder, mock_config
    ):
        """Test that PostgreSQL upsert is called with correct data."""
        from src.processors.indexer import index_processors
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="PG Test",
            slug="pg-test",
            category="analytics",
            headquarters="DE",
            data_categories=["user_id", "event_data"],
            processing_purposes=["analytics"],
            data_locations=["eu"],
            transfer_mechanism="none_required",
            dpa_url="https://example.com/dpa"
        )

        index_processors(
            qdrant=mock_qdrant_client,
            pg_conn=mock_pg_connection,
            embedder=mock_embedder,
            config=mock_config,
            profiles=[profile]
        )

        # Verify cursor.execute was called for the INSERT
        cursor = mock_pg_connection.cursor.return_value.__enter__.return_value
        cursor.execute.assert_called()

    def test_index_processors_qdrant_upsert_payload(
        self, mock_qdrant_client, mock_pg_connection, mock_embedder, mock_config
    ):
        """Test that Qdrant upsert has correct payload structure."""
        from src.processors.indexer import index_processors
        from src.processors.profiles import ProcessorProfile

        profile = ProcessorProfile(
            name="Qdrant Test",
            slug="qdrant-test",
            category="crm",
            headquarters="US",
            data_categories=["name", "email"],
            processing_purposes=["crm"],
            data_locations=["us"],
            transfer_mechanism="dpf"
        )

        index_processors(
            qdrant=mock_qdrant_client,
            pg_conn=mock_pg_connection,
            embedder=mock_embedder,
            config=mock_config,
            profiles=[profile]
        )

        call_kwargs = mock_qdrant_client.upsert.call_args.kwargs
        points = call_kwargs["points"]
        assert len(points) == 1

        payload = points[0].payload
        assert payload["slug"] == "qdrant-test"
        assert payload["name"] == "Qdrant Test"
        assert payload["category"] == "crm"
        assert payload["headquarters"] == "US"
        assert "text" in payload

    def test_get_processor_count(self, mock_qdrant_client):
        """Test getting processor count from Qdrant."""
        from src.processors.indexer import get_processor_count

        mock_qdrant_client.get_collection.return_value.points_count = 50

        count = get_processor_count(mock_qdrant_client)

        assert count == 50

    def test_get_processor_count_error(self, mock_qdrant_client):
        """Test that get_processor_count returns 0 on error."""
        from src.processors.indexer import get_processor_count

        mock_qdrant_client.get_collection.side_effect = Exception("Collection not found")

        count = get_processor_count(mock_qdrant_client)

        assert count == 0

    def test_get_postgres_processor_count(self, mock_pg_connection):
        """Test getting processor count from PostgreSQL."""
        from src.processors.indexer import get_postgres_processor_count

        cursor = mock_pg_connection.cursor.return_value.__enter__.return_value
        cursor.fetchone.return_value = (42,)

        count = get_postgres_processor_count(mock_pg_connection)

        assert count == 42

    def test_get_postgres_processor_count_error(self, mock_pg_connection):
        """Test that get_postgres_processor_count returns 0 on error."""
        from src.processors.indexer import get_postgres_processor_count

        mock_pg_connection.cursor.side_effect = Exception("Connection error")

        count = get_postgres_processor_count(mock_pg_connection)

        assert count == 0


class TestSearchProcessors:
    """Tests for processor search functionality."""

    @pytest.fixture
    def mock_qdrant_client(self):
        """Create a mock Qdrant client with search results."""
        client = Mock()
        return client

    @pytest.fixture
    def mock_embedder(self):
        """Create a mock embedding provider."""
        embedder = Mock()
        embedder.embed = Mock(return_value=[[0.1] * 3072])
        return embedder

    def test_search_processors(self, mock_qdrant_client, mock_embedder):
        """Test searching for processors."""
        from src.processors.indexer import search_processors

        # Mock search results
        mock_hit = Mock()
        mock_hit.payload = {
            "slug": "stripe",
            "name": "Stripe",
            "category": "payment",
            "data_categories": ["payment_card"],
            "transfer_mechanism": "dpf"
        }
        mock_hit.score = 0.95
        mock_qdrant_client.search.return_value = [mock_hit]

        results = search_processors(
            qdrant=mock_qdrant_client,
            embedder=mock_embedder,
            query="payment processing",
            limit=5
        )

        assert len(results) == 1
        assert results[0]["slug"] == "stripe"
        assert results[0]["name"] == "Stripe"
        assert results[0]["score"] == 0.95
        mock_embedder.embed.assert_called_once_with(["payment processing"])

    def test_search_processors_empty_results(self, mock_qdrant_client, mock_embedder):
        """Test search with no results."""
        from src.processors.indexer import search_processors

        mock_qdrant_client.search.return_value = []

        results = search_processors(
            qdrant=mock_qdrant_client,
            embedder=mock_embedder,
            query="nonexistent service",
            limit=5
        )

        assert len(results) == 0


class TestProcessorModule:
    """Tests for the processors module __init__.py exports."""

    def test_module_exports(self):
        """Test that all expected symbols are exported from the module."""
        from src.processors import (
            ProcessorProfile,
            PROCESSOR_PROFILES,
            index_processors,
            create_processor_collection,
            PROCESSOR_COLLECTION,
        )

        assert ProcessorProfile is not None
        assert PROCESSOR_PROFILES is not None
        assert callable(index_processors)
        assert callable(create_processor_collection)
        assert isinstance(PROCESSOR_COLLECTION, str)


class TestConfigProcessorsMode:
    """Tests for processors mode configuration."""

    def test_config_accepts_processors_mode(self):
        """Test that Config accepts 'processors' as a valid mode."""
        import os
        from src.config import Config

        # Set environment variable
        os.environ["MODE"] = "processors"

        try:
            config = Config()
            assert config.mode == "processors"
        finally:
            del os.environ["MODE"]

    def test_validate_processors_required(self):
        """Test processors-specific validation."""
        from src.config import Config

        config = Config()
        config.openai_api_key = ""
        config.postgres_dsn = ""

        with pytest.raises(ValueError) as exc_info:
            config.validate_processors_required()

        assert "OPENAI_API_KEY" in str(exc_info.value)
        assert "POSTGRES_DSN" in str(exc_info.value)

    def test_validate_processors_required_success(self):
        """Test processors validation passes with required keys."""
        from src.config import Config

        config = Config()
        config.openai_api_key = "test-key"
        config.postgres_dsn = "postgresql://localhost/test"

        # Should not raise
        config.validate_processors_required()
