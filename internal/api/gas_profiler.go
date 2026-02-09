package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/sirupsen/logrus"
)

// Verify interface compliance at compile time.
var _ http.Handler = (*GasProfilerHandler)(nil)

// GasProfilerHandler handles gas profiler simulation requests.
// It proxies requests to network-specific Erigon nodes with xatu RPC endpoints.
// Supports multiple endpoints per network with round-robin load balancing.
type GasProfilerHandler struct {
	cfg    *config.GasProfilerConfig
	client *http.Client
	logger logrus.FieldLogger

	// Round-robin counters per network
	counters   map[string]*atomic.Uint64
	countersMu sync.RWMutex
}

// NewGasProfilerHandler creates a new gas profiler handler.
func NewGasProfilerHandler(cfg *config.GasProfilerConfig, logger logrus.FieldLogger) *GasProfilerHandler {
	// Initialize counters for each network
	counters := make(map[string]*atomic.Uint64)

	for _, network := range cfg.GetNetworks() {
		counters[network] = &atomic.Uint64{}
	}

	return &GasProfilerHandler{
		cfg:      cfg,
		client:   cfg.HTTPClient(),
		logger:   logger.WithField("handler", "gas_profiler"),
		counters: counters,
	}
}

// getEndpoint returns an endpoint for the network using round-robin selection.
func (h *GasProfilerHandler) getEndpoint(network string) *config.GasProfilerEndpoint {
	endpoints := h.cfg.GetEndpointsForNetwork(network)
	if len(endpoints) == 0 {
		return nil
	}

	if len(endpoints) == 1 {
		return endpoints[0]
	}

	// Round-robin selection
	h.countersMu.RLock()
	counter := h.counters[network]
	h.countersMu.RUnlock()

	if counter == nil {
		return endpoints[0]
	}

	idx := counter.Add(1) - 1

	return endpoints[idx%uint64(len(endpoints))]
}

// jsonRPCRequest represents a JSON-RPC request.
type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      int         `json:"id"`
}

// jsonRPCResponse represents a JSON-RPC response.
type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

// jsonRPCError represents a JSON-RPC error.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// SimulateBlockRequest is the REST request for block simulation.
type SimulateBlockRequest struct {
	BlockNumber uint64                 `json:"blockNumber"`
	GasSchedule map[string]interface{} `json:"gasSchedule"`
}

// SimulateTransactionRequest is the REST request for transaction simulation.
type SimulateTransactionRequest struct {
	TransactionHash string                 `json:"transactionHash"`
	BlockNumber     uint64                 `json:"blockNumber,omitempty"`
	GasSchedule     map[string]interface{} `json:"gasSchedule"`
}

// ServeHTTP routes requests to the appropriate handler method.
func (h *GasProfilerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract network from path
	network := r.PathValue("network")
	if network == "" {
		h.errorResponse(w, http.StatusBadRequest, "network parameter required")

		return
	}

	// Get endpoint for network (round-robin if multiple)
	endpoint := h.getEndpoint(network)
	if endpoint == nil {
		h.errorResponse(w, http.StatusNotFound, fmt.Sprintf("network %s not configured for gas profiler", network))

		return
	}

	// Route based on action path value
	action := r.PathValue("action")

	switch action {
	case "simulate-block":
		h.handleSimulateBlock(w, r, endpoint)
	case "simulate-transaction":
		h.handleSimulateTx(w, r, endpoint)
	case "gas-schedule":
		h.handleGasSchedule(w, r, endpoint)
	default:
		h.errorResponse(w, http.StatusNotFound, fmt.Sprintf("unknown action: %s", action))
	}
}

// handleSimulateBlock handles POST /api/v1/gas-profiler/{network}/simulate-block.
func (h *GasProfilerHandler) handleSimulateBlock(w http.ResponseWriter, r *http.Request, endpoint *config.GasProfilerEndpoint) {
	if r.Method != http.MethodPost {
		h.errorResponse(w, http.StatusMethodNotAllowed, "POST required")

		return
	}

	var req SimulateBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))

		return
	}

	// Build JSON-RPC request
	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "xatu_simulateBlockGas",
		Params: []interface{}{
			map[string]interface{}{
				"blockNumber": req.BlockNumber,
				"gasSchedule": req.GasSchedule,
			},
		},
		ID: 1,
	}

	h.proxyRPC(w, r, endpoint, &rpcReq)
}

// handleSimulateTx handles POST /api/v1/gas-profiler/{network}/simulate-transaction.
func (h *GasProfilerHandler) handleSimulateTx(w http.ResponseWriter, r *http.Request, endpoint *config.GasProfilerEndpoint) {
	if r.Method != http.MethodPost {
		h.errorResponse(w, http.StatusMethodNotAllowed, "POST required")

		return
	}

	var req SimulateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.errorResponse(w, http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))

		return
	}

	// Build JSON-RPC request
	params := map[string]interface{}{
		"transactionHash": req.TransactionHash,
		"gasSchedule":     req.GasSchedule,
	}

	if req.BlockNumber != 0 {
		params["blockNumber"] = req.BlockNumber
	}

	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "xatu_simulateTransactionGas",
		Params:  []interface{}{params},
		ID:      1,
	}

	h.proxyRPC(w, r, endpoint, &rpcReq)
}

// handleGasSchedule handles GET /api/v1/gas-profiler/{network}/gas-schedule.
// Query params:
//   - block: block number (required) - determines which fork's gas parameters to return
func (h *GasProfilerHandler) handleGasSchedule(w http.ResponseWriter, r *http.Request, endpoint *config.GasProfilerEndpoint) {
	if r.Method != http.MethodGet {
		h.errorResponse(w, http.StatusMethodNotAllowed, "GET required")

		return
	}

	// Parse block number from query params
	blockStr := r.URL.Query().Get("block")
	if blockStr == "" {
		h.errorResponse(w, http.StatusBadRequest, "block query parameter is required")

		return
	}

	blockNumber, err := strconv.ParseUint(blockStr, 10, 64)
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, "invalid block number")

		return
	}

	// Build JSON-RPC request with block number as direct argument
	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "xatu_getGasSchedule",
		Params:  []interface{}{blockNumber},
		ID:      1,
	}

	h.proxyRPC(w, r, endpoint, &rpcReq)
}

// proxyRPC sends a JSON-RPC request to the endpoint and returns the result.
func (h *GasProfilerHandler) proxyRPC(w http.ResponseWriter, r *http.Request, endpoint *config.GasProfilerEndpoint, rpcReq *jsonRPCRequest) {
	// Encode request
	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		h.logger.WithError(err).Error("Failed to encode RPC request")
		h.errorResponse(w, http.StatusInternalServerError, "internal error")

		return
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint.URL, bytes.NewReader(reqBody))
	if err != nil {
		h.logger.WithError(err).Error("Failed to create HTTP request")
		h.errorResponse(w, http.StatusInternalServerError, "internal error")

		return
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := h.client.Do(httpReq)
	if err != nil {
		h.logger.WithError(err).WithField("endpoint", endpoint.Name).Error("Failed to send RPC request")
		h.errorResponse(w, http.StatusBadGateway, "upstream error")

		return
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.WithError(err).Error("Failed to read RPC response")
		h.errorResponse(w, http.StatusBadGateway, "upstream error")

		return
	}

	// Parse JSON-RPC response
	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		h.logger.WithError(err).Error("Failed to parse RPC response")
		h.errorResponse(w, http.StatusBadGateway, "invalid upstream response")

		return
	}

	// Check for RPC error
	if rpcResp.Error != nil {
		h.logger.WithFields(logrus.Fields{
			"code":    rpcResp.Error.Code,
			"message": rpcResp.Error.Message,
		}).Warn("RPC error from upstream")
		h.errorResponse(w, http.StatusBadRequest, rpcResp.Error.Message)

		return
	}

	// Return just the result
	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(rpcResp.Result); err != nil {
		h.logger.WithError(err).Error("Failed to write response")
	}

	h.logger.WithFields(logrus.Fields{
		"network": endpoint.Network,
		"method":  rpcReq.Method,
	}).Debug("Proxied RPC request")
}

// errorResponse writes a JSON error response.
func (h *GasProfilerHandler) errorResponse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	resp := map[string]string{"error": message}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		h.logger.WithError(err).Error("Failed to encode error response")
	}
}
