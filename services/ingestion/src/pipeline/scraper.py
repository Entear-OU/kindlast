import time
from firecrawl import FirecrawlApp
from src.config import Config
from src.utils.logging import get_logger

log = get_logger(__name__)


class Scraper:
    def __init__(self, config: Config):
        self.app = FirecrawlApp(api_key=config.firecrawl_api_key)
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
                result = self.app.scrape_url(
                    url,
                    params={
                        "formats": ["markdown"],
                        "includeTags": ["article", "main", "section", ".content"],
                        "excludeTags": ["nav", "footer", "header", ".cookie-banner"],
                        "onlyMainContent": True,
                        "timeout": 30000,
                    }
                )
                if result.get("markdown"):
                    results.append({
                        "url": url,
                        "markdown": result["markdown"],
                        "title": result.get("metadata", {}).get("title", ""),
                        "scraped_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                    })
            else:
                crawl_result = self.app.crawl_url(
                    url,
                    params={
                        "limit": 50,                  # max pages per crawl
                        "maxDepth": depth,
                        "formats": ["markdown"],
                        "onlyMainContent": True,
                        "excludePaths": ["/search", "/login", "/register"],
                    }
                )
                for page in crawl_result.get("data", []):
                    if page.get("markdown") and len(page["markdown"]) > 200:
                        results.append({
                            "url": page.get("metadata", {}).get("sourceURL", url),
                            "markdown": page["markdown"],
                            "title": page.get("metadata", {}).get("title", ""),
                            "scraped_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
                        })

            time.sleep(self.delay_ms / 1000)
        except Exception as e:
            log.error("scrape_failed", url=url, error=str(e))

        return results
