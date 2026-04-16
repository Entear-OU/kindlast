"""Tests for source registry module."""
import pytest
from pydantic import ValidationError


class TestRegSource:
    """Tests for RegSource model."""

    def test_valid_source(self):
        """Test creating a valid source."""
        from src.sources import RegSource

        source = RegSource(
            name="Test Source",
            url="https://example.com",
            topic=["gdpr"],
            tier=1,
            crawl_depth=1,
            language="en",
            rss_feed=None,
            jurisdiction="eu"
        )

        assert source.name == "Test Source"
        assert source.url == "https://example.com"
        assert source.topic == ["gdpr"]
        assert source.tier == 1
        assert source.crawl_depth == 1
        assert source.language == "en"
        assert source.rss_feed is None
        assert source.jurisdiction == "eu"

    def test_immutable_source(self):
        """Test that sources are immutable (frozen)."""
        from src.sources import RegSource

        source = RegSource(
            name="Test Source",
            url="https://example.com",
            topic=["gdpr"],
            tier=1,
            crawl_depth=1,
            language="en",
            jurisdiction="eu"
        )

        with pytest.raises(ValidationError):
            source.name = "Modified"

    def test_invalid_url(self):
        """Test that invalid URLs are rejected."""
        from src.sources import RegSource

        with pytest.raises(ValidationError):
            RegSource(
                name="Test",
                url="not-a-url",  # Missing http(s)://
                topic=["gdpr"],
                tier=1,
                crawl_depth=1,
                language="en",
                jurisdiction="eu"
            )

    def test_invalid_tier(self):
        """Test that invalid tiers are rejected."""
        from src.sources import RegSource

        with pytest.raises(ValidationError):
            RegSource(
                name="Test",
                url="https://example.com",
                topic=["gdpr"],
                tier=4,  # Invalid: must be 1, 2, or 3
                crawl_depth=1,
                language="en",
                jurisdiction="eu"
            )

    def test_invalid_topic(self):
        """Test that invalid topics are rejected."""
        from src.sources import RegSource

        with pytest.raises(ValidationError):
            RegSource(
                name="Test",
                url="https://example.com",
                topic=["invalid_topic"],  # Must be "gdpr" or "ai_act"
                tier=1,
                crawl_depth=1,
                language="en",
                jurisdiction="eu"
            )

    def test_empty_topic_list(self):
        """Test that empty topic list is rejected."""
        from src.sources import RegSource

        with pytest.raises(ValidationError):
            RegSource(
                name="Test",
                url="https://example.com",
                topic=[],  # Must have at least one topic
                tier=1,
                crawl_depth=1,
                language="en",
                jurisdiction="eu"
            )

    def test_invalid_language_format(self):
        """Test that invalid language codes are rejected."""
        from src.sources import RegSource

        with pytest.raises(ValidationError):
            RegSource(
                name="Test",
                url="https://example.com",
                topic=["gdpr"],
                tier=1,
                crawl_depth=1,
                language="eng",  # Must be 2 characters
                jurisdiction="eu"
            )

    def test_multiple_topics(self):
        """Test source with multiple topics."""
        from src.sources import RegSource

        source = RegSource(
            name="Multi-topic Source",
            url="https://example.com",
            topic=["gdpr", "ai_act"],
            tier=1,
            crawl_depth=2,
            language="en",
            jurisdiction="eu"
        )

        assert "gdpr" in source.topic
        assert "ai_act" in source.topic


class TestSourceRegistry:
    """Tests for the source registry and helper functions."""

    def test_sources_count(self):
        """Test that we have exactly 22 sources."""
        from src.sources import SOURCES

        assert len(SOURCES) == 22

    def test_tier_1_count(self):
        """Test that tier 1 has exactly 10 sources."""
        from src.sources import get_sources_by_tier

        tier_1 = get_sources_by_tier(max_tier=1)
        assert len(tier_1) == 10

    def test_tier_2_cumulative_count(self):
        """Test that tier 1+2 has exactly 19 sources."""
        from src.sources import get_sources_by_tier

        tier_1_and_2 = get_sources_by_tier(max_tier=2)
        assert len(tier_1_and_2) == 19

    def test_tier_3_includes_all(self):
        """Test that max_tier=3 includes all 22 sources."""
        from src.sources import SOURCES, get_sources_by_tier

        all_sources = get_sources_by_tier(max_tier=3)
        assert len(all_sources) == len(SOURCES)

    def test_invalid_tier_raises(self):
        """Test that invalid tier values raise ValueError."""
        from src.sources import get_sources_by_tier

        with pytest.raises(ValueError):
            get_sources_by_tier(max_tier=0)

        with pytest.raises(ValueError):
            get_sources_by_tier(max_tier=4)

    def test_sources_with_rss(self):
        """Test getting sources with RSS feeds."""
        from src.sources import get_sources_with_rss

        rss_sources = get_sources_with_rss()

        # Should have some RSS sources
        assert len(rss_sources) > 0

        # All returned sources should have RSS feeds
        for source in rss_sources:
            assert source.rss_feed is not None
            assert source.rss_feed.startswith("http")

    def test_sources_by_topic_gdpr(self):
        """Test filtering sources by GDPR topic."""
        from src.sources import get_sources_by_topic

        gdpr_sources = get_sources_by_topic("gdpr")

        assert len(gdpr_sources) > 0
        for source in gdpr_sources:
            assert "gdpr" in source.topic

    def test_sources_by_topic_ai_act(self):
        """Test filtering sources by AI Act topic."""
        from src.sources import get_sources_by_topic

        ai_sources = get_sources_by_topic("ai_act")

        assert len(ai_sources) > 0
        for source in ai_sources:
            assert "ai_act" in source.topic

    def test_sources_by_jurisdiction(self):
        """Test filtering sources by jurisdiction."""
        from src.sources import get_sources_by_jurisdiction

        eu_sources = get_sources_by_jurisdiction("eu")

        assert len(eu_sources) > 0
        for source in eu_sources:
            assert source.jurisdiction == "eu"

    def test_all_sources_have_required_fields(self):
        """Test that all sources have valid required fields."""
        from src.sources import SOURCES

        for source in SOURCES:
            assert source.name
            assert source.url.startswith("https://")
            assert len(source.topic) > 0
            assert source.tier in [1, 2, 3]
            assert source.crawl_depth in [1, 2]
            assert len(source.language) == 2
            assert len(source.jurisdiction) == 2
