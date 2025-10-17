package api

import (
	"encoding/json"
	"net/http"

	"github.com/ethpandaops/lab-backend/internal/bounds"
	"github.com/sirupsen/logrus"
)

// Verify interface compliance at compile time.
var _ http.Handler = (*BoundsHandler)(nil)

// BoundsHandler handles GET /api/v1/{network}/bounds requests.
type BoundsHandler struct {
	provider bounds.Provider
	logger   logrus.FieldLogger
}

// NewBoundsHandler creates a new bounds handler.
func NewBoundsHandler(provider bounds.Provider, logger logrus.FieldLogger) *BoundsHandler {
	return &BoundsHandler{
		provider: provider,
		logger:   logger.WithField("handler", "bounds"),
	}
}

// ServeHTTP handles the bounds request.
func (h *BoundsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract network from path parameter
	network := r.PathValue("network")
	if network == "" {
		h.logger.Error("Network parameter missing from path")
		http.Error(w, "network parameter required", http.StatusBadRequest)

		return
	}

	// Check if provider is available
	if h.provider == nil {
		h.logger.Error("Bounds provider not available")
		http.Error(w, "bounds service unavailable", http.StatusServiceUnavailable)

		return
	}

	// Get bounds from provider
	boundsData, exists := h.provider.GetBounds(r.Context(), network)
	if !exists {
		h.logger.WithField("network", network).Warn("Bounds not found for network")
		http.Error(w, "network not found or bounds unavailable", http.StatusNotFound)

		return
	}

	// Send JSON response (encode just the tables map)
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(boundsData.Tables); err != nil {
		h.logger.WithError(err).Error("Failed to encode response")
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	h.logger.WithFields(logrus.Fields{
		"network":     network,
		"table_count": len(boundsData.Tables),
	}).Debug("Served bounds request")
}
