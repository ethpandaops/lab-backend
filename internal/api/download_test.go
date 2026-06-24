package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethpandaops/lab-backend/internal/cartographoor"
	"github.com/ethpandaops/lab-backend/internal/config"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider is a minimal cartographoor.Provider that knows a fixed set of networks.
type stubProvider struct {
	networks map[string]*cartographoor.Network
}

func (s *stubProvider) Start(context.Context) error { return nil }
func (s *stubProvider) Stop() error                 { return nil }
func (s *stubProvider) GetNetworks(context.Context) map[string]*cartographoor.Network {
	return s.networks
}

func (s *stubProvider) GetActiveNetworks(context.Context) map[string]*cartographoor.Network {
	return s.networks
}

func (s *stubProvider) GetNetwork(_ context.Context, name string) (*cartographoor.Network, bool) {
	n, ok := s.networks[name]

	return n, ok
}
func (s *stubProvider) NotifyChannel() <-chan struct{} { return nil }

const (
	testBlockRoot     = "0x712f994351d05d72b466bf2a55f70cedbb734284200dceedd0b0346f4393b881"
	testVersionedHash = "0x01247543e38115941fd15ea7cb7082920be4fa0c98e23f2c8b46aa16516eacd8"
)

func newTestHandler(t *testing.T, blockURL, blobPattern string) *DownloadHandler {
	t.Helper()

	cfg := &config.DownloadConfig{
		Enabled:            true,
		BlockArchiveURL:    blockURL,
		BlobArchivePattern: blobPattern,
	}
	require.NoError(t, cfg.Validate())

	provider := &stubProvider{networks: map[string]*cartographoor.Network{
		"mainnet": {Name: "mainnet"},
	}}

	logger := logrus.New()
	logger.SetOutput(io.Discard)

	return NewDownloadHandler(cfg, provider, logger)
}

func TestDownloadHandler_Block(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mainnet/100/" + testBlockRoot + ".json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"fork":"fulu","block":{}}`))
		case "/mainnet/100/" + testBlockRoot + ".ssz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte{0x01, 0x02, 0x03})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	h := newTestHandler(t, upstream.URL, "http://blob.invalid/{network}")

	tests := []struct {
		name             string
		query            string
		wantStatus       int
		wantContentType  string
		wantDispContains string
	}{
		{
			name:             "json block",
			query:            "slot=100&block_root=" + testBlockRoot + "&format=json",
			wantStatus:       http.StatusOK,
			wantContentType:  "application/json",
			wantDispContains: "mainnet_slot_100_712f994351.json",
		},
		{
			name:             "ssz block",
			query:            "slot=100&block_root=" + testBlockRoot + "&format=ssz",
			wantStatus:       http.StatusOK,
			wantContentType:  "application/octet-stream",
			wantDispContains: ".ssz",
		},
		{
			name:            "defaults to json",
			query:           "slot=100&block_root=" + testBlockRoot,
			wantStatus:      http.StatusOK,
			wantContentType: "application/json",
		},
		{
			name:       "missing slot",
			query:      "block_root=" + testBlockRoot,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "bad block root",
			query:      "slot=100&block_root=0xdeadbeef",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "bad format",
			query:      "slot=100&block_root=" + testBlockRoot + "&format=xml",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not archived",
			query:      "slot=999&block_root=" + testBlockRoot,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(h, "/api/v1/download/mainnet/block?"+tt.query, "mainnet", "block")

			assert.Equal(t, tt.wantStatus, rec.Code)

			if tt.wantContentType != "" {
				assert.Equal(t, tt.wantContentType, rec.Header().Get("Content-Type"))
			}

			if tt.wantDispContains != "" {
				assert.Contains(t, rec.Header().Get("Content-Disposition"), tt.wantDispContains)
			}
		})
	}
}

func TestDownloadHandler_Blob(t *testing.T) {
	blobBytes := []byte{0xde, 0xad, 0xbe, 0xef}
	stored := gzipBytes(t, []byte("0x"+hex.EncodeToString(blobBytes)))

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mainnet/"+testVersionedHash+".gz" {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write(stored)

			return
		}

		http.NotFound(w, r)
	}))
	defer upstream.Close()

	// Blob pattern points the per-network host at the upstream test server.
	h := newTestHandler(t, "http://block.invalid", upstream.URL+"/{network}")
	// Rewrite the blob host: pattern becomes upstream.URL/mainnet after replacement.
	h.cfg.BlobArchivePattern = upstream.URL

	rec := serve(h, "/api/v1/download/mainnet/blob?versioned_hash="+testVersionedHash, "mainnet", "blob")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/octet-stream", rec.Header().Get("Content-Type"))
	assert.Contains(t, rec.Header().Get("Content-Disposition"), ".bin")
	assert.Equal(t, blobBytes, rec.Body.Bytes())
}

func TestDownloadHandler_Validation(t *testing.T) {
	h := newTestHandler(t, "http://block.invalid", "http://blob.invalid/{network}")

	tests := []struct {
		name       string
		path       string
		network    string
		kind       string
		wantStatus int
	}{
		{"unknown network", "/api/v1/download/devnet/block?slot=1&block_root=" + testBlockRoot, "devnet", "block", http.StatusNotFound},
		{"unknown kind", "/api/v1/download/mainnet/foo", "mainnet", "foo", http.StatusNotFound},
		{"bad versioned hash", "/api/v1/download/mainnet/blob?versioned_hash=0x00", "mainnet", "blob", http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(h, tt.path, tt.network, tt.kind)
			assert.Equal(t, tt.wantStatus, rec.Code)
		})
	}
}

// serve drives the handler with the Go 1.22 path values set.
func serve(h *DownloadHandler, target, network, kind string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, http.NoBody)
	req.SetPathValue("network", network)
	req.SetPathValue("kind", kind)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return rec
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	_, err := gz.Write(data)
	require.NoError(t, err)
	require.NoError(t, gz.Close())

	return buf.Bytes()
}

func TestDecompressGzip(t *testing.T) {
	original := []byte("0xabcdef")
	compressed := gzipBytes(t, original)

	assert.Equal(t, original, decompressGzip(compressed))
	// Non-gzip input is returned unchanged.
	assert.Equal(t, []byte("plain"), decompressGzip([]byte("plain")))
}

func TestConfigBlobArchiveURLFor(t *testing.T) {
	cfg := &config.DownloadConfig{Enabled: true}
	require.NoError(t, cfg.Validate())

	assert.Equal(t, "https://blob-archive.mainnet.ethpandaops.io", cfg.BlobArchiveURLFor("mainnet"))
	assert.True(t, strings.HasPrefix(cfg.BlockArchiveURL, "https://block-archiver"))
}
