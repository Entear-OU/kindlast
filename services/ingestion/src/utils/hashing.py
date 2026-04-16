import hashlib
import uuid

def get_content_hash(content: str) -> str:
    """Generate SHA-256 hash of content for change detection."""
    return hashlib.sha256(content.encode('utf-8')).hexdigest()

def make_doc_id(source_url: str) -> str:
    """Generate deterministic document ID from URL."""
    return str(uuid.uuid5(uuid.NAMESPACE_URL, source_url))

def make_chunk_id(doc_id: str, chunk_index: int, prefix: str = "chunk") -> str:
    """Generate deterministic chunk ID from doc_id and index."""
    combined = f"{prefix}:{doc_id}:{chunk_index}"
    return str(uuid.uuid5(uuid.NAMESPACE_DNS, combined))
