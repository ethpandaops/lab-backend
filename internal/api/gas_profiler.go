package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/sirupsen/logrus"
)

const (
	// healthCheckTimeout is the HTTP timeout for individual health checks.
	healthCheckTimeout = 5 * time.Second
)

// truncateString returns s truncated to maxLen characters with an ellipsis
// appended when truncation occurs. Useful for safe log output.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

// Verify interface compliance at compile time.
var _ http.Handler = (*GasProfilerHandler)(nil)

// GasProfilerHandler handles gas profiler simulation requests.
// It proxies requests to network-specific Erigon nodes with xatu RPC endpoints.
// Supports multiple endpoints per network with round-robin load balancing.
// A background poller checks each endpoint's sync status via eth_syncing
// and only routes traffic to fully synced nodes.
type GasProfilerHandler struct {
	cfg    *config.GasProfilerConfig
	client *http.Client
	logger logrus.FieldLogger

	// Round-robin counters per network
	counters   map[string]*atomic.Uint64
	countersMu sync.RWMutex

	// Health tracking: keyed by endpoint name, true = synced
	healthy  map[string]bool
	healthMu sync.RWMutex

	// Lifecycle
	stopCh chan struct{}
	wg     sync.WaitGroup
	booted bool
}

// NewGasProfilerHandler creates a new gas profiler handler.
func NewGasProfilerHandler(cfg *config.GasProfilerConfig, logger logrus.FieldLogger) *GasProfilerHandler {
	// Initialize counters for each network
	counters := make(map[string]*atomic.Uint64, len(cfg.GetNetworks()))

	for _, network := range cfg.GetNetworks() {
		counters[network] = &atomic.Uint64{}
	}

	// Initialize all endpoints as unhealthy until first check
	healthy := make(map[string]bool, len(cfg.Endpoints))
	for _, ep := range cfg.Endpoints {
		healthy[ep.Name] = false
	}

	return &GasProfilerHandler{
		cfg:      cfg,
		client:   cfg.HTTPClient(),
		logger:   logger.WithField("handler", "gas_profiler"),
		counters: counters,
		healthy:  healthy,
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background health polling goroutine.
// It runs an initial health check synchronously before returning.
func (h *GasProfilerHandler) Start() {
	// Run first health check immediately so we know status at boot
	h.checkHealth()

	h.wg.Add(1)

	go func() {
		defer h.wg.Done()

		ticker := time.NewTicker(h.cfg.HealthInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				h.checkHealth()
			case <-h.stopCh:
				return
			}
		}
	}()

	h.logger.WithField("interval", h.cfg.HealthInterval).
		Info("Started endpoint health poller")
}

// Stop signals the background poller to stop and waits for it to finish.
func (h *GasProfilerHandler) Stop() {
	close(h.stopCh)
	h.wg.Wait()

	h.logger.Info("Stopped endpoint health poller")
}

// checkHealth polls each endpoint with eth_syncing and updates health status.
func (h *GasProfilerHandler) checkHealth() {
	for _, ep := range h.cfg.Endpoints {
		synced := h.isEndpointSynced(ep)

		h.healthMu.RLock()
		prev := h.healthy[ep.Name]
		h.healthMu.RUnlock()

		if !h.booted || prev != synced {
			h.logger.WithFields(logrus.Fields{
				"endpoint": ep.Name,
				"network":  ep.Network,
				"healthy":  synced,
			}).Info("Endpoint health status changed")
		}

		h.healthMu.Lock()
		h.healthy[ep.Name] = synced
		h.healthMu.Unlock()
	}

	h.booted = true
}

// isEndpointSynced sends an eth_syncing RPC call and returns true if the
// node is fully synced (result is false), or false if syncing/unreachable.
func (h *GasProfilerHandler) isEndpointSynced(ep config.GasProfilerEndpoint) bool {
	log := h.logger.WithField("endpoint", ep.Name)

	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "eth_syncing",
		Params:  []interface{}{},
		ID:      1,
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		log.WithError(err).Warn("Health check: failed to marshal request")

		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		log.WithError(err).Warn("Health check: failed to create HTTP request")

		return false
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(httpReq)
	if err != nil {
		log.WithError(err).Warn("Health check: HTTP request failed")

		return false
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithError(err).Warn("Health check: failed to read response body")

		return false
	}

	if resp.StatusCode != http.StatusOK {
		log.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"body":        truncateString(string(respBody), 200),
		}).Warn("Health check: unexpected HTTP status")

		return false
	}

	var rpcResp jsonRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		log.WithFields(logrus.Fields{
			"error": err.Error(),
			"body":  truncateString(string(respBody), 200),
		}).Warn("Health check: failed to parse JSON-RPC response")

		return false
	}

	if rpcResp.Error != nil {
		log.WithFields(logrus.Fields{
			"rpc_code":    rpcResp.Error.Code,
			"rpc_message": rpcResp.Error.Message,
		}).Warn("Health check: RPC error")

		return false
	}

	result := string(rpcResp.Result)
	if result != "false" {
		log.WithField("result", truncateString(result, 200)).
			Debug("Health check: node is syncing")

		return false
	}

	return true
}

// getEndpoint returns a healthy endpoint for the network using round-robin.
// Returns nil if no healthy endpoints are available.
func (h *GasProfilerHandler) getEndpoint(network string) *config.GasProfilerEndpoint {
	endpoints := h.cfg.GetEndpointsForNetwork(network)
	if len(endpoints) == 0 {
		return nil
	}

	// Filter to healthy endpoints only
	healthy := make([]*config.GasProfilerEndpoint, 0, len(endpoints))

	h.healthMu.RLock()

	for _, ep := range endpoints {
		if h.healthy[ep.Name] {
			healthy = append(healthy, ep)
		}
	}

	h.healthMu.RUnlock()

	if len(healthy) == 0 {
		return nil
	}

	if len(healthy) == 1 {
		return healthy[0]
	}

	// Round-robin selection across healthy endpoints
	h.countersMu.RLock()
	counter := h.counters[network]
	h.countersMu.RUnlock()

	if counter == nil {
		return healthy[0]
	}

	idx := counter.Add(1) - 1

	return healthy[idx%uint64(len(healthy))]
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
	MaxGasLimit bool                   `json:"maxGasLimit,omitempty"`
}

// SimulateTransactionRequest is the REST request for transaction simulation.
type SimulateTransactionRequest struct {
	TransactionHash string                 `json:"transactionHash"`
	BlockNumber     uint64                 `json:"blockNumber,omitempty"`
	GasSchedule     map[string]interface{} `json:"gasSchedule"`
	MaxGasLimit     bool                   `json:"maxGasLimit,omitempty"`
}

// ServeHTTP routes requests to the appropriate handler method.
func (h *GasProfilerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract network from path
	network := r.PathValue("network")
	if network == "" {
		h.errorResponse(w, http.StatusBadRequest, "network parameter required")

		return
	}

	// Get a healthy endpoint for the network (round-robin if multiple)
	endpoint := h.getEndpoint(network)
	if endpoint == nil {
		// Distinguish between "not configured" and "all syncing"
		if len(h.cfg.GetEndpointsForNetwork(network)) > 0 {
			h.errorResponse(w, http.StatusServiceUnavailable,
				fmt.Sprintf("all backends for network %s are currently syncing", network))

			return
		}

		h.errorResponse(w, http.StatusNotFound,
			fmt.Sprintf("network %s not configured for gas profiler", network))

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
	params := map[string]interface{}{
		"blockNumber": req.BlockNumber,
		"gasSchedule": req.GasSchedule,
	}

	if req.MaxGasLimit {
		params["maxGasLimit"] = true
	}

	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "xatu_simulateBlockGas",
		Params:  []interface{}{params},
		ID:      1,
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

	if req.MaxGasLimit {
		params["maxGasLimit"] = true
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
