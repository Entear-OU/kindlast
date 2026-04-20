import time
from firecrawl import Firecrawl
from firecrawl.v2.types import ScrapeOptions
from src.config import Config
from src.utils.logging import get_logger

log = get_logger(__name__)


class Scraper:
    def __init__(self, config: Config):
        self.app = Firecrawl(api_key=config.firecrawl_api_key)
        self.delay_ms = config.firecrawl_delay_ms

    def scrape_url(self, url: str, depth: int = 1) -> list[dict]:
        """
        Scrape a URL and return list of {url, markdown, title, scraped_at}.
        If depth > 1, crawls linked pages one level deep.
        Returns empty list on failure (logs error, does not raise).
        """
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
            log.error("scrape_failed", url=url, error=str(e))

        return results
