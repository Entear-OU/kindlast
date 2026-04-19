#!/bin/bash
#
# Qdrant Collection Setup for Kindlast
# Creates the required vector collections for the RAG pipeline and processor profiles.
#
# Usage:
#   QDRANT_HOST=localhost:6333 ./scripts/seed-qdrant.sh
#
# Collections created:
#   - kindlast_openai_prod: 3072-dim vectors (text-embedding-3-large) + BM25 sparse
#   - kindlast_cohere_prod: 1024-dim vectors (embed-multilingual-v3) + BM25 sparse
#   - kindlast_processors: 3072-dim vectors (processor profile embeddings)

set -e

QDRANT_HOST="${QDRANT_HOST:-localhost:6333}"
QDRANT_URL="http://${QDRANT_HOST}"

echo "Qdrant Collection Setup"
echo "======================="
echo "Target: ${QDRANT_URL}"
echo ""

# Function to check if a collection exists
collection_exists() {
    local collection_name="$1"
    local response
    response=$(curl -s -o /dev/null -w "%{http_code}" "${QDRANT_URL}/collections/${collection_name}")
    [ "$response" = "200" ]
}

# Function to create a collection
create_collection() {
    local collection_name="$1"
    local config="$2"

    if collection_exists "$collection_name"; then
        echo "[SKIP] Collection '${collection_name}' already exists"
        return 0
    fi

    echo "[CREATE] Creating collection '${collection_name}'..."

    local response
    local http_code
    response=$(curl -s -w "\n%{http_code}" -X PUT "${QDRANT_URL}/collections/${collection_name}" \
        -H 'Content-Type: application/json' \
        -d "$config")

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')

    if [ "$http_code" = "200" ]; then
        echo "[OK] Collection '${collection_name}' created successfully"
    else
        echo "[ERROR] Failed to create collection '${collection_name}'"
        echo "HTTP Status: ${http_code}"
        echo "Response: ${body}"
        return 1
    fi
}

# Collection 1: kindlast_openai_prod
# - 3072-dimensional vectors for text-embedding-3-large
# - Cosine distance metric
# - BM25 sparse vectors with IDF modifier for hybrid search
echo ""
echo "1/3 Processing kindlast_openai_prod..."
create_collection "kindlast_openai_prod" '{
    "vectors": {
        "size": 3072,
        "distance": "Cosine"
    },
    "sparse_vectors": {
        "bm25": {
            "modifier": "idf"
        }
    },
    "replication_factor": 1
}'

# Collection 2: kindlast_cohere_prod
# - 1024-dimensional vectors for embed-multilingual-v3
# - Cosine distance metric
# - BM25 sparse vectors with IDF modifier for hybrid search
echo ""
echo "2/3 Processing kindlast_cohere_prod..."
create_collection "kindlast_cohere_prod" '{
    "vectors": {
        "size": 1024,
        "distance": "Cosine"
    },
    "sparse_vectors": {
        "bm25": {
            "modifier": "idf"
        }
    },
    "replication_factor": 1
}'

# Collection 3: kindlast_processors
# - 3072-dimensional vectors (processor profile embeddings using OpenAI)
# - Cosine distance metric
# - No sparse vectors (semantic search only for fuzzy matching)
echo ""
echo "3/3 Processing kindlast_processors..."
create_collection "kindlast_processors" '{
    "vectors": {
        "size": 3072,
        "distance": "Cosine"
    },
    "replication_factor": 1
}'

echo ""
echo "======================="
echo "Qdrant setup complete!"
echo ""

# Verify collections
echo "Verifying collections..."
collections_response=$(curl -s "${QDRANT_URL}/collections")
echo "Collections available:"
echo "$collections_response" | grep -o '"name":"[^"]*"' | sed 's/"name":"//g' | sed 's/"//g' | while read -r name; do
    echo "  - ${name}"
done

echo ""
echo "Done."
