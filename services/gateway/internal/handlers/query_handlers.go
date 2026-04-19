package handlers

import (
	"log/slog"
	"net/http"

	"github.com/entear/kindlast/services/gateway/internal/proxy"
)

type QueryHandler struct {
	ragProxy *proxy.RAGProxy
	logger   *slog.Logger
}

func NewQueryHandler(ragProxy *proxy.RAGProxy, logger *slog.Logger) *QueryHandler {
	return &QueryHandler{
		ragProxy: ragProxy,
		logger:   logger,
	}
}

// Query handles POST /api/v1/query - proxy to RAG service
func (h *QueryHandler) Query(w http.ResponseWriter, r *http.Request) {
	// Delegate to RAG proxy
	h.ragProxy.ProxyRequest(w, r)
}
