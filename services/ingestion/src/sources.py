from pydantic import BaseModel, Field, field_validator
from typing import Optional, Literal


class RegSource(BaseModel):
    """A regulatory source for ingestion."""

    name: str = Field(..., min_length=1, max_length=100, description="Human-readable source name")
    url: str = Field(..., description="Primary URL to scrape")
    topic: list[Literal["gdpr", "ai_act"]] = Field(
        ..., min_length=1,
        description="Topics covered by this source"
    )
    tier: Literal[1, 2, 3] = Field(
        ...,
        description="Priority tier: 1=primary law, 2=national DPAs, 3=supplementary"
    )
    crawl_depth: Literal[1, 2] = Field(
        ...,
        description="Crawl depth: 1=single page, 2=follow links one level"
    )
    language: str = Field(..., pattern=r"^[a-z]{2}$", description="ISO 639-1 language code")
    rss_feed: Optional[str] = Field(default=None, description="RSS feed URL for change detection")
    jurisdiction: str = Field(
        ..., pattern=r"^[a-z]{2}$",
        description="ISO 3166-1 alpha-2 jurisdiction code"
    )

    model_config = {
        "frozen": True,  # Make instances immutable
        "str_strip_whitespace": True,
    }

    @field_validator("url", "rss_feed", mode="before")
    @classmethod
    def validate_url_format(cls, v):
        """Basic URL validation."""
        if v is not None and not v.startswith(("http://", "https://")):
            raise ValueError(f"URL must start with http:// or https://: {v}")
        return v


# TIER 1 — Primary law and major DPAs (10 sources)
TIER_1_SOURCES = [
    RegSource(
        name="EUR-Lex GDPR",
        url="https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679",
        topic=["gdpr"], tier=1, crawl_depth=1, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="EUR-Lex EU AI Act",
        url="https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32024R1689",
        topic=["ai_act"], tier=1, crawl_depth=1, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="EDPB Guidelines",
        url="https://edpb.europa.eu/our-work-tools/general-guidance/guidelines-recommendations-best-practices_en",
        topic=["gdpr"], tier=1, crawl_depth=2, language="en",
        rss_feed="https://edpb.europa.eu/edpb-feed-rss_en",
        jurisdiction="eu"
    ),
    RegSource(
        name="EDPB Opinions",
        url="https://edpb.europa.eu/our-work-tools/our-documents/opinion-art-64_en",
        topic=["gdpr"], tier=1, crawl_depth=2, language="en",
        rss_feed="https://edpb.europa.eu/edpb-feed-rss_en",
        jurisdiction="eu"
    ),
    RegSource(
        name="EU AI Office",
        url="https://digital-strategy.ec.europa.eu/en/policies/ai-office",
        topic=["ai_act"], tier=1, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="CNIL",
        url="https://www.cnil.fr/en/home",
        topic=["gdpr"], tier=1, crawl_depth=2, language="en",
        rss_feed="https://www.cnil.fr/en/rss.xml",
        jurisdiction="fr"
    ),
    RegSource(
        name="ICO",
        url="https://ico.org.uk/for-organisations/",
        topic=["gdpr"], tier=1, crawl_depth=2, language="en",
        rss_feed="https://ico.org.uk/about-the-ico/news-and-events/rss/",
        jurisdiction="uk"
    ),
    RegSource(
        name="DPC Ireland",
        url="https://www.dataprotection.ie/en/organisations",
        topic=["gdpr"], tier=1, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="ie"
    ),
    RegSource(
        name="BfDI Germany",
        url="https://www.bfdi.bund.de/EN/Home/home_node.html",
        topic=["gdpr"], tier=1, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="de"
    ),
    RegSource(
        name="Garante Italy",
        url="https://www.garanteprivacy.it/home/docweb",
        topic=["gdpr", "ai_act"], tier=1, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="it"
    ),
]

# TIER 2 — National DPAs and supporting bodies (9 sources)
TIER_2_SOURCES = [
    RegSource(
        name="AP Netherlands",
        url="https://autoriteitpersoonsgegevens.nl/en",
        topic=["gdpr"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="nl"
    ),
    RegSource(
        name="AEPD Spain",
        url="https://www.aepd.es/en",
        topic=["gdpr"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="es"
    ),
    RegSource(
        name="DSB Austria",
        url="https://www.dsb.gv.at/",
        topic=["gdpr"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="at"
    ),
    RegSource(
        name="UODO Poland",
        url="https://uodo.gov.pl/en",
        topic=["gdpr"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="pl"
    ),
    RegSource(
        name="AKI Estonia",
        url="https://www.aki.ee/en",
        topic=["gdpr"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="ee"
    ),
    RegSource(
        name="ENISA",
        url="https://www.enisa.europa.eu/publications",
        topic=["gdpr", "ai_act"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="CJEU Case Law",
        url="https://curia.europa.eu/juris/recherche.jsf",
        topic=["gdpr"], tier=2, crawl_depth=1, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="EUR-Lex AI Act Amendments",
        url="https://eur-lex.europa.eu/search.html?text=AI+Act&scope=EURLEX&type=quick&lang=en",
        topic=["ai_act"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="AI Act Timeline",
        url="https://artificialintelligenceact.eu/",
        topic=["ai_act"], tier=2, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
]

# TIER 3 — Supplementary sources (3 sources)
TIER_3_SOURCES = [
    RegSource(
        name="EDPB ChatGPT Task Force",
        url="https://edpb.europa.eu/our-work-tools/our-documents/other-documents/report-work-undertaken-chatgpt-taskforce_en",
        topic=["gdpr", "ai_act"], tier=3, crawl_depth=1, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="Council of Europe AI Convention",
        url="https://www.coe.int/en/web/artificial-intelligence/the-framework-convention-on-ai",
        topic=["ai_act"], tier=3, crawl_depth=2, language="en",
        rss_feed=None, jurisdiction="eu"
    ),
    RegSource(
        name="IAPP News",
        url="https://iapp.org/news/",
        topic=["gdpr", "ai_act"], tier=3, crawl_depth=2, language="en",
        rss_feed="https://iapp.org/feed/",
        jurisdiction="us"
    ),
]

# All sources combined
SOURCES: list[RegSource] = TIER_1_SOURCES + TIER_2_SOURCES + TIER_3_SOURCES


def get_sources_by_tier(max_tier: int = 3) -> list[RegSource]:
    """Get sources up to and including the specified tier."""
    if max_tier < 1 or max_tier > 3:
        raise ValueError(f"max_tier must be between 1 and 3, got {max_tier}")
    return [s for s in SOURCES if s.tier <= max_tier]


def get_sources_with_rss() -> list[RegSource]:
    """Get sources that have RSS feeds for change detection."""
    return [s for s in SOURCES if s.rss_feed is not None]


def get_sources_by_topic(topic: Literal["gdpr", "ai_act"]) -> list[RegSource]:
    """Get sources covering a specific topic."""
    return [s for s in SOURCES if topic in s.topic]


def get_sources_by_jurisdiction(jurisdiction: str) -> list[RegSource]:
    """Get sources for a specific jurisdiction."""
    return [s for s in SOURCES if s.jurisdiction == jurisdiction]
