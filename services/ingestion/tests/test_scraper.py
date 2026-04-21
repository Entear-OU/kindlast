"""Tests for scraper module."""
import pytest
from unittest.mock import Mock, patch, MagicMock


class TestFirecrawlScraper:
    """Tests for FirecrawlScraper class."""

    def test_provider_name(self):
        """Test that provider name is correct."""
        from src.pipeline.scraper import FirecrawlScraper

        mock_config = Mock()
        mock_config.firecrawl_api_key = "test-key"
        mock_config.firecrawl_delay_ms = 500

        with patch("src.pipeline.scraper.Firecrawl"):
            scraper = FirecrawlScraper(mock_config)
            assert scraper.provider_name == "firecrawl"

    def test_scrape_single_page(self):
        """Test scraping a single page."""
        from src.pipeline.scraper import FirecrawlScraper

        mock_config = Mock()
        mock_config.firecrawl_api_key = "test-key"
        mock_config.firecrawl_delay_ms = 0

        mock_doc = Mock()
        mock_doc.markdown = "# Test Page\n\nSome content here."
        mock_doc.metadata = Mock(title="Test Page")

        mock_firecrawl = Mock()
        mock_firecrawl.scrape.return_value = mock_doc

        with patch("src.pipeline.scraper.Firecrawl", return_value=mock_firecrawl):
            scraper = FirecrawlScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=1)

            assert len(results) == 1
            assert results[0]["url"] == "https://example.com"
            assert results[0]["markdown"] == "# Test Page\n\nSome content here."
            assert results[0]["title"] == "Test Page"
            assert "scraped_at" in results[0]

    def test_scrape_multi_page_crawl(self):
        """Test crawling multiple pages."""
        from src.pipeline.scraper import FirecrawlScraper

        mock_config = Mock()
        mock_config.firecrawl_api_key = "test-key"
        mock_config.firecrawl_delay_ms = 0

        mock_doc1 = Mock()
        mock_doc1.markdown = "# Page 1\n\n" + "x" * 300
        mock_doc1.metadata = Mock(source_url="https://example.com/page1", title="Page 1")

        mock_doc2 = Mock()
        mock_doc2.markdown = "# Page 2\n\n" + "y" * 300
        mock_doc2.metadata = Mock(source_url="https://example.com/page2", title="Page 2")

        mock_crawl_job = Mock()
        mock_crawl_job.data = [mock_doc1, mock_doc2]

        mock_firecrawl = Mock()
        mock_firecrawl.crawl.return_value = mock_crawl_job

        with patch("src.pipeline.scraper.Firecrawl", return_value=mock_firecrawl):
            scraper = FirecrawlScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=2)

            assert len(results) == 2
            assert results[0]["url"] == "https://example.com/page1"
            assert results[1]["url"] == "https://example.com/page2"

    def test_scrape_filters_short_content(self):
        """Test that short content is filtered in multi-page crawl."""
        from src.pipeline.scraper import FirecrawlScraper

        mock_config = Mock()
        mock_config.firecrawl_api_key = "test-key"
        mock_config.firecrawl_delay_ms = 0

        mock_doc_long = Mock()
        mock_doc_long.markdown = "# Long Page\n\n" + "x" * 300
        mock_doc_long.metadata = Mock(source_url="https://example.com/long", title="Long")

        mock_doc_short = Mock()
        mock_doc_short.markdown = "Short"  # Less than 200 chars
        mock_doc_short.metadata = Mock(source_url="https://example.com/short", title="Short")

        mock_crawl_job = Mock()
        mock_crawl_job.data = [mock_doc_long, mock_doc_short]

        mock_firecrawl = Mock()
        mock_firecrawl.crawl.return_value = mock_crawl_job

        with patch("src.pipeline.scraper.Firecrawl", return_value=mock_firecrawl):
            scraper = FirecrawlScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=2)

            # Only long content should be included
            assert len(results) == 1
            assert results[0]["url"] == "https://example.com/long"

    def test_scrape_error_handling(self):
        """Test that errors are logged but not raised."""
        from src.pipeline.scraper import FirecrawlScraper

        mock_config = Mock()
        mock_config.firecrawl_api_key = "test-key"
        mock_config.firecrawl_delay_ms = 0

        mock_firecrawl = Mock()
        mock_firecrawl.scrape.side_effect = Exception("Network error")

        with patch("src.pipeline.scraper.Firecrawl", return_value=mock_firecrawl):
            scraper = FirecrawlScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=1)

            # Should return empty list, not raise
            assert results == []


class TestTavilyScraper:
    """Tests for TavilyScraper class."""

    def test_provider_name(self):
        """Test that provider name is correct."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 500
        mock_config.tavily_extract_depth = "basic"

        with patch("src.pipeline.scraper.TavilyClient"):
            scraper = TavilyScraper(mock_config)
            assert scraper.provider_name == "tavily"

    def test_extract_single_page(self):
        """Test extracting a single page."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 0
        mock_config.tavily_extract_depth = "basic"

        mock_client = Mock()
        mock_client.extract.return_value = {
            "results": [
                {
                    "url": "https://example.com",
                    "raw_content": "# Test Page\n\nSome content here. " + "x" * 200
                }
            ],
            "failed_results": []
        }

        with patch("src.pipeline.scraper.TavilyClient", return_value=mock_client):
            scraper = TavilyScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=1)

            assert len(results) == 1
            assert results[0]["url"] == "https://example.com"
            assert "# Test Page" in results[0]["markdown"]
            assert results[0]["title"] == "Test Page"
            assert "scraped_at" in results[0]

    def test_crawl_multi_page(self):
        """Test crawling multiple pages."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 0
        mock_config.tavily_extract_depth = "basic"

        mock_client = Mock()
        mock_client.crawl.return_value = {
            "results": [
                {
                    "url": "https://example.com/page1",
                    "raw_content": "# Page 1\n\n" + "x" * 300
                },
                {
                    "url": "https://example.com/page2",
                    "raw_content": "# Page 2\n\n" + "y" * 300
                }
            ]
        }

        with patch("src.pipeline.scraper.TavilyClient", return_value=mock_client):
            scraper = TavilyScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=2)

            assert len(results) == 2
            assert results[0]["url"] == "https://example.com/page1"
            assert results[1]["url"] == "https://example.com/page2"

    def test_extract_filters_short_content(self):
        """Test that short content is filtered."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 0
        mock_config.tavily_extract_depth = "basic"

        mock_client = Mock()
        mock_client.extract.return_value = {
            "results": [
                {
                    "url": "https://example.com",
                    "raw_content": "Short"  # Less than 200 chars
                }
            ],
            "failed_results": []
        }

        with patch("src.pipeline.scraper.TavilyClient", return_value=mock_client):
            scraper = TavilyScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=1)

            # Should filter out short content
            assert len(results) == 0

    def test_extract_depth_config(self):
        """Test that extract_depth is passed correctly."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 0
        mock_config.tavily_extract_depth = "advanced"

        mock_client = Mock()
        mock_client.extract.return_value = {"results": [], "failed_results": []}

        with patch("src.pipeline.scraper.TavilyClient", return_value=mock_client):
            scraper = TavilyScraper(mock_config)
            scraper.scrape_url("https://example.com", depth=1)

            mock_client.extract.assert_called_once_with(
                urls=["https://example.com"],
                extract_depth="advanced"
            )

    def test_extract_error_handling(self):
        """Test that errors are logged but not raised."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 0
        mock_config.tavily_extract_depth = "basic"

        mock_client = Mock()
        mock_client.extract.side_effect = Exception("API error")

        with patch("src.pipeline.scraper.TavilyClient", return_value=mock_client):
            scraper = TavilyScraper(mock_config)
            results = scraper.scrape_url("https://example.com", depth=1)

            # Should return empty list, not raise
            assert results == []

    def test_extract_title_from_h1(self):
        """Test title extraction from H1 heading."""
        from src.pipeline.scraper import TavilyScraper

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 0
        mock_config.tavily_extract_depth = "basic"

        with patch("src.pipeline.scraper.TavilyClient"):
            scraper = TavilyScraper(mock_config)

            # H1 heading
            content = "# My Title\n\nSome content"
            assert scraper._extract_title(content) == "My Title"

            # No heading, use first line
            content = "First line\nSecond line"
            assert scraper._extract_title(content) == "First line"

            # Empty content
            content = ""
            assert scraper._extract_title(content) == ""


class TestScraperProvider:
    """Tests for ScraperProvider interface."""

    def test_abstract_methods(self):
        """Test that ScraperProvider is abstract."""
        from src.pipeline.scraper import ScraperProvider

        # Cannot instantiate abstract class
        with pytest.raises(TypeError):
            ScraperProvider()

    def test_firecrawl_implements_interface(self):
        """Test that FirecrawlScraper implements all required methods."""
        from src.pipeline.scraper import FirecrawlScraper, ScraperProvider

        mock_config = Mock()
        mock_config.firecrawl_api_key = "test-key"
        mock_config.firecrawl_delay_ms = 500

        with patch("src.pipeline.scraper.Firecrawl"):
            scraper = FirecrawlScraper(mock_config)

            assert isinstance(scraper, ScraperProvider)
            assert hasattr(scraper, "scrape_url")
            assert hasattr(scraper, "provider_name")

    def test_tavily_implements_interface(self):
        """Test that TavilyScraper implements all required methods."""
        from src.pipeline.scraper import TavilyScraper, ScraperProvider

        mock_config = Mock()
        mock_config.tavily_api_key = "tvly-test-key"
        mock_config.tavily_delay_ms = 500
        mock_config.tavily_extract_depth = "basic"

        with patch("src.pipeline.scraper.TavilyClient"):
            scraper = TavilyScraper(mock_config)

            assert isinstance(scraper, ScraperProvider)
            assert hasattr(scraper, "scrape_url")
            assert hasattr(scraper, "provider_name")


class TestLegacyAlias:
    """Tests for backwards compatibility."""

    def test_scraper_alias(self):
        """Test that Scraper alias points to FirecrawlScraper."""
        from src.pipeline.scraper import Scraper, FirecrawlScraper

        assert Scraper is FirecrawlScraper
