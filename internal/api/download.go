package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/sirupsen/logrus"
)

// Verify interface compliance at compile time.
var _ http.Handler = (*DownloadHandler)(nil)

// hash32Pattern matches a 0x-prefixed 32-byte hex string (block root, versioned hash).
var hash32Pattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// DownloadHandler proxies beacon block and blob downloads from the upstream
// archives, re-serving them as attachments so browsers download rather than
// render them inline. It handles GET /api/v1/download/{network}/{kind}.
type DownloadHandler struct {
	cfg        *config.DownloadConfig
	provider   cartographoor.Provider
	httpClient *http.Client
	logger     logrus.FieldLogger
}

// NewDownloadHandler creates a new download proxy handler.
func NewDownloadHandler(
	cfg *config.DownloadConfig,
	provider cartographoor.Provider,
	logger logrus.FieldLogger,
) *DownloadHandler {
	return &DownloadHandler{
		cfg:        cfg,
		provider:   provider,
		httpClient: cfg.HTTPClient(),
		logger:     logger.WithField("handler", "download"),
	}
}

// ServeHTTP routes the download request to the block or blob handler.
func (h *DownloadHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	network := r.PathValue("network")
	if network == "" {
		h.errorResponse(w, http.StatusBadRequest, "network parameter required")

		return
	}

	if h.provider == nil {
		h.errorResponse(w, http.StatusServiceUnavailable, "download service unavailable")

		return
	}

	if _, exists := h.provider.GetNetwork(r.Context(), network); !exists {
		h.errorResponse(w, http.StatusNotFound, fmt.Sprintf("network %s not found", network))

		return
	}

	switch kind := r.PathValue("kind"); kind {
	case "block":
		h.handleBlock(w, r, network)
	case "blob":
		h.handleBlob(w, r, network)
	default:
		h.errorResponse(w, http.StatusNotFound, fmt.Sprintf("unknown download kind: %s", kind))
	}
}

// handleBlock streams a beacon block from the block archive as an attachment.
func (h *DownloadHandler) handleBlock(w http.ResponseWriter, r *http.Request, network string) {
	q := r.URL.Query()

	slot, err := strconv.ParseUint(q.Get("slot"), 10, 64)
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, "valid slot parameter required")

		return
	}

	blockRoot := q.Get("block_root")
	if !hash32Pattern.MatchString(blockRoot) {
		h.errorResponse(w, http.StatusBadRequest, "valid block_root parameter required (0x-prefixed 32-byte hex)")

		return
	}

	format := q.Get("format")
	if format == "" {
		format = "json"
	}

	contentType := ""

	switch format {
	case "json":
		contentType = "application/json"
	case "ssz":
		contentType = "application/octet-stream"
	default:
		h.errorResponse(w, http.StatusBadRequest, "format must be 'json' or 'ssz'")

		return
	}

	upstream := fmt.Sprintf("%s/%s/%d/%s.%s", h.cfg.BlockArchiveURL, network, slot, blockRoot, format)

	resp, err := h.fetchUpstream(r.Context(), upstream)
	if err != nil {
		h.logger.WithError(err).WithField("url", upstream).Error("Block archive request failed")
		h.errorResponse(w, http.StatusBadGateway, "failed to reach block archive")

		return
	}
	defer resp.Body.Close()

	if !h.upstreamOK(w, resp, "block") {
		return
	}

	filename := fmt.Sprintf("%s_slot_%d_%s.%s", network, slot, shortHash(blockRoot), format)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.WithError(err).Warn("Failed streaming block download")
	}
}

// handleBlob fetches a blob sidecar from the blob archive, decodes the stored
// gzip+hex payload back to raw bytes, and serves it as an attachment.
func (h *DownloadHandler) handleBlob(w http.ResponseWriter, r *http.Request, network string) {
	versionedHash := r.URL.Query().Get("versioned_hash")
	if !hash32Pattern.MatchString(versionedHash) {
		h.errorResponse(w, http.StatusBadRequest, "valid versioned_hash parameter required (0x-prefixed 32-byte hex)")

		return
	}

	upstream := fmt.Sprintf("%s/%s/%s.gz", h.cfg.BlobArchiveURLFor(network), network, versionedHash)

	resp, err := h.fetchUpstream(r.Context(), upstream)
	if err != nil {
		h.logger.WithError(err).WithField("url", upstream).Error("Blob archive request failed")
		h.errorResponse(w, http.StatusBadGateway, "failed to reach blob archive")

		return
	}
	defer resp.Body.Close()

	if !h.upstreamOK(w, resp, "blob") {
		return
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		h.logger.WithError(err).Warn("Failed reading blob response")
		h.errorResponse(w, http.StatusBadGateway, "failed to read blob from archive")

		return
	}

	// The archive stores blobs as gzip of a 0x-prefixed hex string. Decompress
	// when gzipped, then decode the hex back to the raw blob bytes.
	payload := decompressGzip(raw)
	hexStr := strings.TrimPrefix(strings.TrimSpace(string(payload)), "0x")

	blobBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		// Not hex — serve the decompressed payload as-is rather than failing.
		h.logger.WithError(err).Debug("Blob payload not hex; serving raw payload")

		filename := fmt.Sprintf("%s_blob_%s.txt", network, shortHash(versionedHash))
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)

		if _, werr := w.Write(payload); werr != nil {
			h.logger.WithError(werr).Warn("Failed writing blob payload")
		}

		return
	}

	filename := fmt.Sprintf("%s_blob_%s.bin", network, shortHash(versionedHash))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write(blobBytes); err != nil {
		h.logger.WithError(err).Warn("Failed writing blob download")
	}
}

// fetchUpstream issues a GET request to an upstream archive.
func (h *DownloadHandler) fetchUpstream(ctx context.Context, url string) (*http.Response, error) {
	// G704: url is built from a registry-validated network plus regex-checked
	// hashes and a fixed-base archive URL; it is not user-controlled host input.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody) //nolint:gosec // see above
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := h.httpClient.Do(req) //nolint:gosec // url validated; see fetchUpstream comment
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	return resp, nil
}

// upstreamOK reports whether the upstream response can be served. On a non-OK
// status it writes the appropriate error response and returns false.
func (h *DownloadHandler) upstreamOK(w http.ResponseWriter, resp *http.Response, artifact string) bool {
	switch resp.StatusCode {
	case http.StatusOK:
		return true
	case http.StatusNotFound:
		h.errorResponse(w, http.StatusNotFound, fmt.Sprintf("%s not found in archive", artifact))

		return false
	default:
		h.errorResponse(
			w,
			http.StatusBadGateway,
			fmt.Sprintf("archive returned status %d", resp.StatusCode),
		)

		return false
	}
}

// errorResponse writes a JSON error response.
func (h *DownloadHandler) errorResponse(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		h.logger.WithError(err).Error("Failed to encode error response")
	}
}

// decompressGzip returns the gzip-decompressed data, or the input unchanged if
// it is not gzip-compressed.
func decompressGzip(data []byte) []byte {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	defer gz.Close()

	out, err := io.ReadAll(gz)
	if err != nil {
		return data
	}

	return out
}

// shortHash returns a short, filename-safe form of a 0x-prefixed hash.
func shortHash(hash string) string {
	trimmed := strings.TrimPrefix(hash, "0x")
	if len(trimmed) > 10 {
		return trimmed[:10]
	}

	return trimmed
}
