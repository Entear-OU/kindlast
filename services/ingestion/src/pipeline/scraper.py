import time
from abc import ABC, abstractmethod
from firecrawl import Firecrawl
from firecrawl.v2.types import ScrapeOptions
from tavily import TavilyClient
from src.config import Config
from src.utils.logging import get_logger

log = get_logger(__name__)


class ScraperProvider(ABC):
    """Abstract base class for web scraping providers."""

    @abstractmethod
    def scrape_url(self, url: str, depth: int = 1) -> list[dict]:
        """
        Scrape a URL and return list of {url, markdown, title, scraped_at}.
        If depth > 1, crawls linked pages.
        Returns empty list on failure (logs error, does not raise).
        """
        pass

    @property
    @abstractmethod
    def provider_name(self) -> str:
        """Return the name of the scraping provider."""
        pass


class FirecrawlScraper(ScraperProvider):
    """Firecrawl-based web scraper."""

    def __init__(self, config: Config):
        self.app = Firecrawl(api_key=config.firecrawl_api_key)
        self.delay_ms = config.firecrawl_delay_ms

        log.info("scraper_initialized", provider="firecrawl")

    @property
    def provider_name(self) -> str:
        return "firecrawl"

    def scrape_url(self, url: str, depth: int = 1) -> list[dict]:
        results = []
        try:
            if depth == 1:
                # Single page scrape - returns a Document object
                doc = self.app.scrape(
                    url,
                    formats=["markdown"],
                    only_main_content=True,
                    timeout=30000,
                )
                if doc.markdown:
                    results.append({
                        "url": url,
                        "markdown": doc.markdown,
                        "title": doc.metadata.title if doc.metadata else "",
                        "scraped_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                    })
            else:
                # Multi-page crawl - returns a CrawlJob object
                crawl_job = self.app.crawl(
                    url,
                    limit=50,
                    max_discovery_depth=depth,
                    exclude_paths=["/search", "/login", "/register"],
                    scrape_options=ScrapeOptions(
                        formats=["markdown"],
                        only_main_content=True,
                    )
                )
                # crawl_job.data contains list of Document objects
                for doc in crawl_job.data or []:
                    if doc.markdown and len(doc.markdown) > 200:
                        source_url = doc.metadata.source_url if doc.metadata else url
                        title = doc.metadata.title if doc.metadata else ""
                        results.append({
                            "url": source_url or url,
                            "markdown": doc.markdown,
                            "title": title,
                            "scraped_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                        })

            time.sleep(self.delay_ms / 1000)
        except Exception as e:
            log.error("scrape_failed", url=url, provider="firecrawl", error=str(e))

        return results


class TavilyScraper(ScraperProvider):
    """Tavily-based web scraper optimized for AI/RAG workflows."""

    def __init__(self, config: Config):
        self.client = TavilyClient(api_key=config.tavily_api_key)
        self.delay_ms = config.tavily_delay_ms
        self.extract_depth = config.tavily_extract_depth

        log.info("scraper_initialized", provider="tavily", extract_depth=self.extract_depth)

    @property
    def provider_name(self) -> str:
        return "tavily"

    def scrape_url(self, url: str, depth: int = 1) -> list[dict]:
        results = []
        try:
            if depth == 1:
                # Single page extraction
                response = self.client.extract(
                    urls=[url],
                    extract_depth=self.extract_depth,
                )

                for result in response.get("results", []):
                    raw_content = result.get("raw_content", "")
                    if raw_content and len(raw_content) > 200:
                        results.append({
                            "url": result.get("url", url),
                            "markdown": raw_content,
                            "title": self._extract_title(raw_content),
                            "scraped_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                        })

                # Log any failed extractions
                for failed in response.get("failed_results", []):
                    log.warning("tavily_extraction_failed",
                               url=failed.get("url"),
                               error=failed.get("error"))
            else:
                # Multi-page crawl using Tavily's crawl API
                response = self.client.crawl(
                    url=url,
                    max_depth=depth,
                    max_breadth=20,
                    limit=50,
                    extract_depth=self.extract_depth,
                    exclude_paths=["/search", "/login", "/register"],
                )

                for result in response.get("results", []):
                    raw_content = result.get("raw_content", "")
                    if raw_content and len(raw_content) > 200:
                        results.append({
                            "url": result.get("url", url),
                            "markdown": raw_content,
                            "title": self._extract_title(raw_content),
                            "scraped_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                        })

            time.sleep(self.delay_ms / 1000)
        except Exception as e:
            log.error("scrape_failed", url=url, provider="tavily", error=str(e))

        return results

    def _extract_title(self, content: str) -> str:
        """Extract title from markdown content (first H1 or first line)."""
        lines = content.strip().split("\n")
        for line in lines[:10]:  # Check first 10 lines
            line = line.strip()
            if line.startswith("# "):
                return line[2:].strip()
        # Fallback: first non-empty line
        for line in lines[:5]:
            line = line.strip()
            if line:
                return line[:100]  # Truncate long first lines
        return ""


# Legacy alias for backwards compatibility
Scraper = FirecrawlScraper
