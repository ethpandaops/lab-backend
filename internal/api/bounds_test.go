package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ethpandaops/lab-backend/internal/bounds"
	boundsmocks "github.com/ethpandaops/lab-backend/internal/bounds/mocks"
)

func TestBoundsHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		network        string
		mockBounds     *bounds.BoundsData
		mockFound      bool
		providerNil    bool
		expectedStatus int
		validateResp   func(t *testing.T, tables map[string]bounds.TableBounds)
	}{
		{
			name:    "valid network returns bounds",
			network: "mainnet",
			mockBounds: &bounds.BoundsData{
				Tables: map[string]bounds.TableBounds{
					"beacon_block": {Min: 100, Max: 200},
					"beacon_state": {Min: 50, Max: 150},
				},
				LastUpdated: time.Now(),
			},
			mockFound:      true,
			expectedStatus: http.StatusOK,
			validateResp: func(t *testing.T, tables map[string]bounds.TableBounds) {
				t.Helper()

				require.NotNil(t, tables)
				assert.Len(t, tables, 2)
				assert.Contains(t, tables, "beacon_block")
				assert.Contains(t, tables, "beacon_state")
				assert.Equal(t, int64(100), tables["beacon_block"].Min)
				assert.Equal(t, int64(200), tables["beacon_block"].Max)
			},
		},
		{
			name:           "network not found returns 404",
			network:        "nonexistent",
			mockBounds:     nil,
			mockFound:      false,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "missing network parameter returns 400",
			network:        "",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "nil provider returns 503",
			network:        "mainnet",
			providerNil:    true,
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			var provider bounds.Provider

			if !tt.providerNil && tt.network != "" {
				mockProvider := boundsmocks.NewMockProvider(ctrl)
				mockProvider.EXPECT().
					GetBounds(gomock.Any(), tt.network).
					Return(tt.mockBounds, tt.mockFound).
					Times(1)
				provider = mockProvider
			}

			logger := logrus.New()
			logger.SetOutput(io.Discard)
			handler := NewBoundsHandler(provider, logger)

			// Create request with path value
			req := httptest.NewRequest(http.MethodGet, "/api/v1/"+tt.network+"/bounds", http.NoBody)
			req.SetPathValue("network", tt.network)

			rec := httptest.NewRecorder()

			// Execute
			handler.ServeHTTP(rec, req)

			// Assert status
			assert.Equal(t, tt.expectedStatus, rec.Code)

			// Validate response if expected to succeed
			if tt.expectedStatus == http.StatusOK && tt.validateResp != nil {
				var tables map[string]bounds.TableBounds

				err := json.NewDecoder(rec.Body).Decode(&tables)
				require.NoError(t, err)

				tt.validateResp(t, tables)
			}
		})
	}
}

func TestBoundsHandler_ContentType(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProvider := boundsmocks.NewMockProvider(ctrl)
	mockProvider.EXPECT().
		GetBounds(gomock.Any(), "mainnet").
		Return(&bounds.BoundsData{
			Tables:      map[string]bounds.TableBounds{},
			LastUpdated: time.Now(),
		}, true).
		Times(1)

	logger := logrus.New()
	logger.SetOutput(io.Discard)
	handler := NewBoundsHandler(mockProvider, logger)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/mainnet/bounds", http.NoBody)
	req.SetPathValue("network", "mainnet")

	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
}
