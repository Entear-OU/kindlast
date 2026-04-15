# PRD 02 — Ingestion Pipeline

**Agent**: Ingestion agent  
**DEPENDS ON**: `01-infrastructure.md` complete (Qdrant, PostgreSQL running, collections created)  
**Produces**: Working Python service that indexes all 22 regulatory sources into Qdrant daily  

---

## Overview

The ingestion service is a Python application that runs as a Kubernetes CronJob. It scrapes regulatory sources via Firecrawl, parses documents via Unstructured.io, chunks them semantically, embeds with OpenAI, and upserts into Qdrant. It uses content-hash diffing so only changed documents are re-processed.

---

## Service structure

```
services/ingestion/
├── src/
│   ├── __init__.py
│   ├── main.py               # entrypoint — reads MODE env var
│   ├── config.py             # all config from env vars
│   ├── sources.py            # source registry (22 URLs + metadata)
│   ├── pipeline/
│   │   ├── __init__.py
│   │   ├── scraper.py        # Firecrawl wrapper
│   │   ├── parser.py         # Unstructured.io wrapper
│   │   ├── chunker.py        # semantic chunking logic
│   │   ├── embedder.py       # OpenAI + Cohere embedding providers
│   │   └── indexer.py        # Qdrant upsert + orphan cleanup
│   ├── db/
│   │   ├── __init__.py
│   │   └── postgres.py       # ingestion_log, parent_chunks, dead_letter
│   └── utils/
│       ├── hashing.py        # content hash, chunk ID generation
│       └── logging.py        # structlog setup
├── tests/
│   ├── test_chunker.py
│   ├── test_embedder.py
│   └── test_indexer.py
├── requirements.txt
└── pyproject.toml
```

---

## Task 1 — Configuration

Create `services/ingestion/src/config.py`:

```python
from dataclasses import dataclass
import os

@dataclass
class Config:
    # Mode
    mode: str = os.getenv("MODE", "incremental")  # incremental | full | single

    # AI providers
    openai_api_key: str = os.getenv("OPENAI_API_KEY", "")
    cohere_api_key: str = os.getenv("COHERE_API_KEY", "")
    firecrawl_api_key: str = os.getenv("FIRECRAWL_API_KEY", "")

    # Infrastructure
    qdrant_host: str = os.getenv("QDRANT_HOST", "localhost")
    qdrant_port: int = int(os.getenv("QDRANT_PORT", "6333"))
    qdrant_api_key: str = os.getenv("QDRANT_API_KEY", "")
    postgres_dsn: str = os.getenv("POSTGRES_DSN", "")

    # Collections
    openai_collection: str = "kindlast_openai_prod"
    cohere_collection: str = "kindlast_cohere_prod"

    # Chunking
    max_chunk_chars: int = 1000        # child chunk size
    max_parent_chars: int = 3000       # parent chunk size
    chunk_overlap: int = 100

    # Embedding
    openai_embedding_model: str = "text-embedding-3-large"
    openai_embedding_dims: int = 3072
    cohere_embedding_model: str = "embed-multilingual-v3.0"
    cohere_embedding_dims: int = 1024
    embedding_batch_size: int = 100

    # Rate limiting
    firecrawl_delay_ms: int = 500      # between scrape calls
    openai_rpm_limit: int = 3000       # requests per minute

    def validate(self):
        assert self.openai_api_key, "OPENAI_API_KEY required"
        assert self.firecrawl_api_key, "FIRECRAWL_API_KEY required"
        assert self.postgres_dsn, "POSTGRES_DSN required"
```

### Acceptance criteria
- [ ] `Config()` instantiates from environment variables without error
- [ ] `Config().validate()` raises `AssertionError` when required keys missing

---

## Task 2 — Source registry

Create `services/ingestion/src/sources.py` — the complete list of 22 regulatory sources:

```python
from dataclasses import dataclass, field
from typing import Optional

@dataclass
class RegSource:
    name: str
    url: str
    topic: list[str]           # ["gdpr"] | ["ai_act"] | ["gdpr", "ai_act"]
    tier: int                  # 1 | 2 | 3
    crawl_depth: int           # 1 = single page, 2 = follow links one level
    language: str              # "en" | "fr" | "de" | "it"
    rss_feed: Optional[str]    # if available, use as change signal
    jurisdiction: str          # "eu" | "fr" | "ie" | "de" | "it" | "nl" | etc.

SOURCES: list[RegSource] = [
    # TIER 1 — Primary law
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
    # TIER 2 — National DPAs and supporting bodies
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
    # TIER 3 — Supplementary
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

def get_sources_by_tier(max_tier: int = 3) -> list[RegSource]:
    return [s for s in SOURCES if s.tier <= max_tier]

def get_sources_with_rss() -> list[RegSource]:
    return [s for s in SOURCES if s.rss_feed is not None]
```

### Acceptance criteria
- [ ] `len(SOURCES) == 22`
- [ ] All sources have required fields populated
- [ ] `get_sources_by_tier(1)` returns exactly 10 sources

---

## Task 3 — Scraper (Firecrawl wrapper)

Create `services/ingestion/src/pipeline/scraper.py`:

```python
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
```

### Acceptance criteria
- [ ] `Scraper.scrape_url("https://eur-lex.europa.eu/...")` returns non-empty list
- [ ] Failed scrape returns `[]`, does not raise
- [ ] Returned dicts all have `url`, `markdown`, `title`, `scraped_at` keys

---

## Task 4 — Parser (Unstructured.io wrapper)

Create `services/ingestion/src/pipeline/parser.py`:

```python
from unstructured.partition.auto import partition
from unstructured.partition.text import partition_text
from unstructured.documents.elements import (
    Title, NarrativeText, ListItem, Table, Header
)
import tempfile, os

class Parser:
    def parse_markdown(self, markdown: str, source_url: str) -> list[dict]:
        """
        Parse markdown string into structured elements.
        Returns list of {type, text, metadata}.
        Strips: Footer, PageBreak, Header (page headers not section headers).
        Preserves: Title, NarrativeText, ListItem, Table.
        """
        with tempfile.NamedTemporaryFile(mode='w', suffix='.md', delete=False) as f:
            f.write(markdown)
            tmp_path = f.name
        
        try:
            elements = partition(filename=tmp_path, strategy="auto")
            structured = []
            
            for el in elements:
                el_type = type(el).__name__
                
                # skip page-level headers and footers
                if el_type in ("Footer", "PageBreak", "PageNumber"):
                    continue
                
                # skip very short elements (navigation artifacts)
                if len(el.text.strip()) < 20:
                    continue
                
                structured.append({
                    "type": el_type,
                    "text": el.text.strip(),
                    "is_title": el_type in ("Title", "Header"),
                    "is_table": el_type == "Table",
                    "source_url": source_url,
                })
            
            return structured
        finally:
            os.unlink(tmp_path)
```

### Acceptance criteria
- [ ] Parsing EDPB guideline markdown returns >10 elements
- [ ] `Table` elements are preserved intact
- [ ] Elements shorter than 20 chars are excluded
- [ ] No `Footer` or `PageBreak` elements in output

---

## Task 5 — Chunker (semantic chunking)

Create `services/ingestion/src/pipeline/chunker.py`:

```python
from src.config import Config

class Chunker:
    """
    Creates parent and child chunks from parsed elements.
    
    Strategy:
    - Group elements by Title boundaries → parent chunks
    - Split parent chunks into child chunks for embedding
    - Child chunks inherit parent metadata (title, section)
    - Tables are always atomic (never split)
    """
    
    def __init__(self, config: Config):
        self.max_child = config.max_chunk_chars
        self.max_parent = config.max_parent_chars
        self.overlap = config.chunk_overlap
    
    def chunk(self, elements: list[dict], doc_id: str) -> tuple[list[dict], list[dict]]:
        """
        Returns (parent_chunks, child_chunks).
        Each parent_chunk: {id, text, doc_id, section_title, source_url, chunk_index}
        Each child_chunk: {id, text, parent_id, doc_id, chunk_index, source_url,
                           section_title, is_table, embedding_model}
        """
        parents = self._build_parents(elements, doc_id)
        children = self._split_children(parents, doc_id)
        return parents, children
    
    def _build_parents(self, elements: list[dict], doc_id: str) -> list[dict]:
        parents = []
        current_section = "Introduction"
        current_text = ""
        parent_idx = 0
        source_url = elements[0]["source_url"] if elements else ""
        
        for el in elements:
            if el["is_title"]:
                if current_text.strip():
                    parents.append(self._make_parent(
                        current_text, current_section, source_url, doc_id, parent_idx
                    ))
                    parent_idx += 1
                current_section = el["text"]
                current_text = el["text"] + "\n\n"
            elif el["is_table"]:
                # tables flush current parent and become their own
                if current_text.strip():
                    parents.append(self._make_parent(
                        current_text, current_section, source_url, doc_id, parent_idx
                    ))
                    parent_idx += 1
                    current_text = ""
                parents.append(self._make_parent(
                    el["text"], current_section, source_url, doc_id, parent_idx,
                    is_table=True
                ))
                parent_idx += 1
            else:
                current_text += el["text"] + "\n\n"
                if len(current_text) > self.max_parent:
                    parents.append(self._make_parent(
                        current_text, current_section, source_url, doc_id, parent_idx
                    ))
                    parent_idx += 1
                    # carry overlap into next parent
                    current_text = current_text[-self.overlap:]
        
        if current_text.strip():
            parents.append(self._make_parent(
                current_text, current_section, source_url, doc_id, parent_idx
            ))
        
        return parents
    
    def _split_children(self, parents: list[dict], doc_id: str) -> list[dict]:
        children = []
        child_idx = 0
        
        for parent in parents:
            if parent.get("is_table"):
                # table is always a single atomic child
                children.append({
                    "id": self._child_id(doc_id, child_idx),
                    "text": parent["text"],
                    "parent_id": parent["id"],
                    "doc_id": doc_id,
                    "chunk_index": child_idx,
                    "source_url": parent["source_url"],
                    "section_title": parent["section_title"],
                    "is_table": True,
                })
                child_idx += 1
                continue
            
            text = parent["text"]
            start = 0
            while start < len(text):
                end = min(start + self.max_child, len(text))
                # try to break at sentence boundary
                if end < len(text):
                    last_period = text.rfind('. ', start, end)
                    if last_period > start + self.max_child // 2:
                        end = last_period + 1
                
                chunk_text = text[start:end].strip()
                if len(chunk_text) > 50:  # skip tiny fragments
                    children.append({
                        "id": self._child_id(doc_id, child_idx),
                        "text": chunk_text,
                        "parent_id": parent["id"],
                        "doc_id": doc_id,
                        "chunk_index": child_idx,
                        "source_url": parent["source_url"],
                        "section_title": parent["section_title"],
                        "is_table": False,
                    })
                    child_idx += 1
                
                start = end - self.overlap if end < len(text) else end
        
        return children
    
    def _make_parent(self, text, section_title, source_url, doc_id, idx, is_table=False):
        from src.utils.hashing import make_chunk_id
        return {
            "id": make_chunk_id(doc_id, idx, prefix="parent"),
            "text": text.strip(),
            "doc_id": doc_id,
            "section_title": section_title,
            "source_url": source_url,
            "chunk_index": idx,
            "is_table": is_table,
        }
    
    def _child_id(self, doc_id, idx):
        from src.utils.hashing import make_chunk_id
        return make_chunk_id(doc_id, idx, prefix="child")
```

### Acceptance criteria
- [ ] Tables are always single child chunks
- [ ] No child chunk exceeds `max_chunk_chars`
- [ ] Every child has a valid `parent_id` that exists in parents list
- [ ] Overlapping text carries over between child chunks

---

## Task 6 — Embedder (dual provider)

Create `services/ingestion/src/pipeline/embedder.py`:

```python
from abc import ABC, abstractmethod
from openai import OpenAI
import cohere
from src.utils.logging import get_logger

log = get_logger(__name__)

class EmbeddingProvider(ABC):
    @abstractmethod
    def embed(self, texts: list[str]) -> list[list[float]]:
        pass
    
    @property
    @abstractmethod
    def collection_name(self) -> str:
        pass
    
    @property
    @abstractmethod
    def dimensions(self) -> int:
        pass

class OpenAIEmbedder(EmbeddingProvider):
    def __init__(self, api_key: str, model: str = "text-embedding-3-large",
                 dimensions: int = 3072, batch_size: int = 100):
        self.client = OpenAI(api_key=api_key)
        self.model = model
        self._dims = dimensions
        self.batch_size = batch_size

    @property
    def collection_name(self) -> str:
        return "kindlast_openai_prod"
    
    @property
    def dimensions(self) -> int:
        return self._dims

    def embed(self, texts: list[str]) -> list[list[float]]:
        all_vectors = []
        for i in range(0, len(texts), self.batch_size):
            batch = texts[i:i + self.batch_size]
            response = self.client.embeddings.create(
                model=self.model,
                input=batch,
                dimensions=self._dims
            )
            all_vectors.extend([r.embedding for r in response.data])
        return all_vectors

class CohereEmbedder(EmbeddingProvider):
    def __init__(self, api_key: str, model: str = "embed-multilingual-v3.0",
                 batch_size: int = 96):
        self.client = cohere.Client(api_key=api_key)
        self.model = model
        self.batch_size = batch_size
    
    @property
    def collection_name(self) -> str:
        return "kindlast_cohere_prod"
    
    @property
    def dimensions(self) -> int:
        return 1024

    def embed(self, texts: list[str]) -> list[list[float]]:
        all_vectors = []
        for i in range(0, len(texts), self.batch_size):
            batch = texts[i:i + self.batch_size]
            response = self.client.embed(
                texts=batch,
                model=self.model,
                input_type="search_document"
            )
            all_vectors.extend(response.embeddings)
        return all_vectors
```

### Acceptance criteria
- [ ] `OpenAIEmbedder.embed(["test"])` returns list of 1 vector with 3072 dimensions
- [ ] `CohereEmbedder.embed(["test"])` returns list of 1 vector with 1024 dimensions
- [ ] Batch size is respected — 200 texts in batches of 100 makes 2 API calls

---

## Task 7 — Indexer (Qdrant upsert + orphan cleanup)

Create `services/ingestion/src/pipeline/indexer.py`:

```python
import uuid
from qdrant_client import QdrantClient
from qdrant_client.models import (
    PointStruct, Filter, FieldCondition, MatchValue, FilterSelector
)
from src.utils.hashing import get_content_hash
from src.utils.logging import get_logger

log = get_logger(__name__)

class Indexer:
    def __init__(self, qdrant_host: str, qdrant_port: int, api_key: str = ""):
        self.client = QdrantClient(host=qdrant_host, port=qdrant_port, api_key=api_key or None)
    
    def should_reprocess(self, source_url: str, new_hash: str, collection: str) -> bool:
        """Returns True if document has changed or is new."""
        results, _ = self.client.scroll(
            collection_name=collection,
            scroll_filter=Filter(must=[
                FieldCondition(key="source_url", match=MatchValue(value=source_url))
            ]),
            limit=1,
            with_payload=True,
            with_vectors=False
        )
        if not results:
            return True
        stored_hash = results[0].payload.get("content_hash", "")
        return stored_hash != new_hash
    
    def upsert_chunks(self, child_chunks: list[dict], vectors: list[list[float]],
                      collection: str, content_hash: str, 
                      embedding_model: str, scraped_at: str):
        """Upsert child chunks with their vectors into Qdrant."""
        points = []
        for chunk, vector in zip(child_chunks, vectors):
            points.append(PointStruct(
                id=str(uuid.UUID(chunk["id"])),
                vector=vector,
                payload={
                    "text": chunk["text"],
                    "source_url": chunk["source_url"],
                    "doc_id": chunk["doc_id"],
                    "parent_id": chunk["parent_id"],
                    "chunk_index": chunk["chunk_index"],
                    "section_title": chunk["section_title"],
                    "is_table": chunk["is_table"],
                    "content_hash": content_hash,
                    "embedding_model": embedding_model,
                    "scraped_at": scraped_at,
                }
            ))
        
        # batch upsert in groups of 100
        for i in range(0, len(points), 100):
            self.client.upsert(collection_name=collection, points=points[i:i+100])
        
        log.info("chunks_upserted", collection=collection, count=len(points))
    
    def delete_orphans(self, doc_id: str, new_chunk_ids: set[str], collection: str):
        """Remove chunks from Qdrant that no longer exist in the document."""
        existing_ids = set()
        offset = None
        
        while True:
            results, next_offset = self.client.scroll(
                collection_name=collection,
                scroll_filter=Filter(must=[
                    FieldCondition(key="doc_id", match=MatchValue(value=doc_id))
                ]),
                limit=100,
                offset=offset,
                with_payload=False,
                with_vectors=False
            )
            for point in results:
                existing_ids.add(str(point.id))
            offset = next_offset
            if offset is None:
                break
        
        orphaned = existing_ids - new_chunk_ids
        if orphaned:
            self.client.delete(
                collection_name=collection,
                points_selector=FilterSelector(
                    filter=Filter(must=[
                        FieldCondition(key="doc_id", match=MatchValue(value=doc_id))
                    ])
                )
            )
            # re-upsert the non-orphaned ones (simpler than selective delete by ID list)
            log.info("orphans_removed", doc_id=doc_id, count=len(orphaned))
```

### Acceptance criteria
- [ ] `should_reprocess` returns True for new URL, False for unchanged content hash
- [ ] Upserted chunks appear in Qdrant: `client.scroll(collection)` returns them
- [ ] Orphan cleanup removes chunks when document shrinks
- [ ] Upsert is idempotent — running twice produces same result

---

## Task 8 — Main pipeline orchestrator

Create `services/ingestion/src/main.py`:

```python
import os, json, time
from src.config import Config
from src.sources import SOURCES, get_sources_by_tier
from src.pipeline.scraper import Scraper
from src.pipeline.parser import Parser
from src.pipeline.chunker import Chunker
from src.pipeline.embedder import OpenAIEmbedder, CohereEmbedder
from src.pipeline.indexer import Indexer
from src.db.postgres import DB
from src.utils.hashing import get_content_hash, make_doc_id
from src.utils.logging import get_logger

log = get_logger(__name__)

def process_document(source_url: str, scrape_result: dict,
                     config: Config, scraper: Scraper, parser: Parser,
                     chunker: Chunker, embedders: list, indexer: Indexer, db: DB):
    """Process a single scraped document through the full pipeline."""
    content = scrape_result["markdown"]
    content_hash = get_content_hash(content)
    doc_id = make_doc_id(source_url)
    
    try:
        # check if any embedder's collection needs update
        needs_update = any(
            indexer.should_reprocess(source_url, content_hash, e.collection_name)
            for e in embedders
        )
        
        if not needs_update:
            log.info("skipping_unchanged", source_url=source_url)
            db.log_ingestion(doc_id, source_url, chunk_count=None,
                           content_hash=content_hash, status="skipped")
            return
        
        # parse and chunk
        elements = parser.parse_markdown(content, source_url)
        parents, children = chunker.chunk(elements, doc_id)
        
        if not children:
            log.warning("no_chunks", source_url=source_url)
            return
        
        # store parent chunks in PostgreSQL
        db.upsert_parent_chunks(parents)
        
        # embed and index in all providers' collections
        texts = [c["text"] for c in children]
        new_chunk_ids = {c["id"] for c in children}
        
        for embedder in embedders:
            vectors = embedder.embed(texts)
            indexer.upsert_chunks(
                children, vectors, embedder.collection_name,
                content_hash=content_hash,
                embedding_model=embedder.model,
                scraped_at=scrape_result["scraped_at"]
            )
            indexer.delete_orphans(doc_id, new_chunk_ids, embedder.collection_name)
        
        db.log_ingestion(doc_id, source_url, chunk_count=len(children),
                        content_hash=content_hash, status="success")
        
        log.info("document_processed",
                source_url=source_url,
                chunk_count=len(children),
                parent_count=len(parents))
    
    except Exception as e:
        log.error("document_failed", source_url=source_url, error=str(e))
        db.log_ingestion(doc_id, source_url, chunk_count=None,
                        content_hash=content_hash, status="failed",
                        error_message=str(e))
        db.add_to_dead_letter(source_url, str(e))

def run():
    config = Config()
    config.validate()
    
    scraper = Scraper(config)
    parser = Parser()
    chunker = Chunker(config)
    embedders = [
        OpenAIEmbedder(config.openai_api_key, config.openai_embedding_model,
                      config.openai_embedding_dims, config.embedding_batch_size),
        CohereEmbedder(config.cohere_api_key, config.cohere_embedding_model,
                      config.embedding_batch_size),
    ]
    indexer = Indexer(config.qdrant_host, config.qdrant_port, config.qdrant_api_key)
    db = DB(config.postgres_dsn)
    
    mode = config.mode
    log.info("ingestion_started", mode=mode)
    
    if mode == "incremental":
        sources = get_sources_by_tier(max_tier=3)
    elif mode == "full":
        sources = SOURCES
    elif mode == "single":
        # for debugging: MODE=single SINGLE_URL=https://...
        url = os.getenv("SINGLE_URL", "")
        assert url, "SINGLE_URL required for single mode"
        sources = [s for s in SOURCES if s.url == url]
    
    for source in sources:
        log.info("processing_source", name=source.name, url=source.url)
        scrape_results = scraper.scrape_url(source.url, source.crawl_depth)
        
        for result in scrape_results:
            process_document(
                result["url"], result, config,
                scraper, parser, chunker, embedders, indexer, db
            )
        
        time.sleep(1)  # courtesy delay between sources
    
    log.info("ingestion_complete", sources_processed=len(sources))

if __name__ == "__main__":
    run()
```

### Acceptance criteria
- [ ] `MODE=single SINGLE_URL=https://eur-lex... python -m src.main` runs without error
- [ ] After run, Qdrant has chunks for the processed URL
- [ ] Re-running immediately shows all documents as "skipped" (hash unchanged)
- [ ] Failed document is logged to `ingestion_dead_letter` table
- [ ] `ingestion_log` table has one row per processed URL

---

## Task 9 — Kubernetes CronJobs

Create `infrastructure/k8s/ingestion/ingestion-cronjob.yaml`:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: ingestion-daily
  namespace: kindlast-ingestion
spec:
  schedule: "0 2 * * *"       # 2am UTC daily
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 5
  jobTemplate:
    spec:
      ttlSecondsAfterFinished: 3600
      backoffLimit: 2
      template:
        spec:
          restartPolicy: OnFailure
          containers:
          - name: ingestion
            image: kindlast/ingestion:latest
            env:
            - name: MODE
              value: incremental
            - name: OPENAI_API_KEY
              valueFrom:
                secretKeyRef:
                  name: ai-provider-keys
                  key: openai-api-key
            - name: COHERE_API_KEY
              valueFrom:
                secretKeyRef:
                  name: ai-provider-keys
                  key: cohere-api-key
            - name: FIRECRAWL_API_KEY
              valueFrom:
                secretKeyRef:
                  name: ai-provider-keys
                  key: firecrawl-api-key
            - name: QDRANT_HOST
              value: qdrant.kindlast-data.svc.cluster.local
            - name: POSTGRES_DSN
              valueFrom:
                secretKeyRef:
                  name: postgres-credentials
                  key: dsn
            resources:
              requests:
                memory: "512Mi"
                cpu: "250m"
              limits:
                memory: "1Gi"
                cpu: "500m"
```

Create `infrastructure/k8s/ingestion/reconcile-cronjob.yaml` — identical but `MODE=full`, schedule `"0 3 * * 0"` (Sunday 3am).

### Acceptance criteria
- [ ] `kubectl create job --from=cronjob/ingestion-daily test-run -n kindlast-ingestion` succeeds
- [ ] Job pod reaches Completed status within 30 minutes
- [ ] Failed job creates entry in `ingestion_dead_letter` table
- [ ] Prometheus alert fires when job fails (configure via PrometheusRule)
