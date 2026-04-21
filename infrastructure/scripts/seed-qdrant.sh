#!/bin/bash
set -e

QDRANT_HOST="${QDRANT_HOST:-localhost:6333}"

# Embedding dimensions (configurable via environment)
EMBEDDING_DIMENSION="${EMBEDDING_DIMENSION:-3072}"
COHERE_EMBEDDING_DIMENSION="${COHERE_EMBEDDING_DIMENSION:-1024}"

echo "Creating Qdrant collections on $QDRANT_HOST..."
echo "Embedding dimensions: ${EMBEDDING_DIMENSION} (OpenAI/local), ${COHERE_EMBEDDING_DIMENSION} (Cohere)"

# Create collection for OpenAI/local embeddings (primary)
echo "Creating kindlast_openai_prod collection..."
curl -s -X PUT "http://$QDRANT_HOST/collections/kindlast_openai_prod" \
  -H 'Content-Type: application/json' \
  -d "{
    \"vectors\": {\"size\": ${EMBEDDING_DIMENSION}, \"distance\": \"Cosine\"},
    \"sparse_vectors\": {\"bm25\": {\"modifier\": \"idf\"}},
    \"replication_factor\": 1
  }" | jq .

# Create collection for Cohere embeddings (fallback)
echo "Creating kindlast_cohere_prod collection..."
curl -s -X PUT "http://$QDRANT_HOST/collections/kindlast_cohere_prod" \
  -H 'Content-Type: application/json' \
  -d "{
    \"vectors\": {\"size\": ${COHERE_EMBEDDING_DIMENSION}, \"distance\": \"Cosine\"},
    \"sparse_vectors\": {\"bm25\": {\"modifier\": \"idf\"}},
    \"replication_factor\": 1
  }" | jq .

echo ""
echo "Qdrant collections created successfully!"
echo "Verify at: http://$QDRANT_HOST/collections"
