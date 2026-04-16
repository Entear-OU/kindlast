from unstructured.partition.auto import partition
from unstructured.partition.text import partition_text
from unstructured.documents.elements import (
    Title, NarrativeText, ListItem, Table, Header
)
import tempfile
import os


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
