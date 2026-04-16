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
