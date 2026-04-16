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
