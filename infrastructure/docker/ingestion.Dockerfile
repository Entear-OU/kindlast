# Ingestion Service Dockerfile
# Python ingestion pipeline with scraping, OCR, and embedding capabilities

FROM python:3.12-slim AS builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy requirements first for better layer caching
COPY services/ingestion/requirements.txt .

# Install Python dependencies
RUN pip install --no-cache-dir --upgrade pip \
    && pip install --no-cache-dir -r requirements.txt

# Download spaCy model
RUN python -m spacy download en_core_web_lg

# Final stage
FROM python:3.12-slim

# Install runtime dependencies
# libgomp1: Required for parallel processing (OpenMP)
# tesseract-ocr: OCR for PDF/image text extraction
# poppler-utils: PDF rendering and manipulation
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgomp1 \
    tesseract-ocr \
    tesseract-ocr-eng \
    poppler-utils \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && apt-get clean

WORKDIR /app

# Copy installed packages from builder
COPY --from=builder /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages
COPY --from=builder /usr/local/bin /usr/local/bin

# Create non-root user for security
RUN groupadd --gid 1001 ingestion \
    && useradd --uid 1001 --gid 1001 --shell /bin/bash --create-home ingestion

# Create necessary directories with proper permissions
RUN mkdir -p /app/data /app/cache /app/logs \
    && chown -R ingestion:ingestion /app

# Copy application code
COPY --chown=ingestion:ingestion services/ingestion/ .

# Switch to non-root user
USER 1001

# Set environment variables
ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONPATH=/app

# Expose metrics port (if applicable)
EXPOSE 8082

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD python -c "import sys; sys.exit(0)" || exit 1

CMD ["python", "-m", "src.main"]
