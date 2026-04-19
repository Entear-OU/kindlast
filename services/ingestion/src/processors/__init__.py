"""Processor profile ingestion module for DPO Copilot."""
from .profiles import ProcessorProfile, PROCESSOR_PROFILES
from .indexer import index_processors, create_processor_collection, PROCESSOR_COLLECTION

__all__ = [
    "ProcessorProfile",
    "PROCESSOR_PROFILES",
    "index_processors",
    "create_processor_collection",
    "PROCESSOR_COLLECTION",
]
