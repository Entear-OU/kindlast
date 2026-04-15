#!/bin/bash
set -e

QDRANT_HOST="${QDRANT_HOST:-localhost:6333}"

echo "Creating Qdrant collections on $QDRANT_HOST..."

# Create collection for OpenAI embeddings (primary)
# text-embedding-3-large produces 3072-dimensional vectors
echo "Creating kindlast_openai_prod collection..."
curl -s -X PUT "http://$QDRANT_HOST/collections/kindlast_openai_prod" \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 3072, "distance": "Cosine"},
    "sparse_vectors": {"bm25": {"modifier": "idf"}},
    "replication_factor": 1
  }' | jq .

# Create collection for Cohere embeddings (fallback)
# embed-multilingual-v3 produces 1024-dimensional vectors
echo "Creating kindlast_cohere_prod collection..."
curl -s -X PUT "http://$QDRANT_HOST/collections/kindlast_cohere_prod" \
  -H 'Content-Type: application/json' \
  -d '{
    "vectors": {"size": 1024, "distance": "Cosine"},
    "sparse_vectors": {"bm25": {"modifier": "idf"}},
    "replication_factor": 1
  }' | jq .

echo ""
echo "Qdrant collections created successfully!"
echo "Verify at: http://$QDRANT_HOST/collections"
