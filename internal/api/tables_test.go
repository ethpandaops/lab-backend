package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ethpandaops/lab-backend/internal/bounds"
	boundsmocks "github.com/ethpandaops/lab-backend/internal/bounds/mocks"
	"github.com/ethpandaops/lab-backend/internal/tables"
)

func TestTablesHandler(t *testing.T) {
	spec, err := os.ReadFile("../tables/testdata/openapi.yaml")
	require.NoError(t, err)

	specFetches := 0

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi.yaml" {
			http.NotFound(w, r)

			return
		}

		specFetches++
		_, _ = w.Write(spec)
	}))
	defer backend.Close()

	targets := map[string]string{"mainnet": backend.URL + "/some/path", "broken": "http://127.0.0.1:1"}
	service := tables.NewService(func(network string) (string, bool) {
		target, ok := targets[network]

		return target, ok
	}, logrus.New())

	ctrl := gomock.NewController(t)
	provider := boundsmocks.NewMockProvider(ctrl)
	provider.EXPECT().GetBounds(gomock.Any(), "mainnet").Return(&bounds.BoundsData{
		Tables:      map[string]bounds.TableBounds{"fct_block_head": {Min: 100, Max: 200}},
		LastUpdated: time.Now(),
	}, true).AnyTimes()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler := NewTablesHandler(service, provider, next, logrus.New())

	serve := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		return rec
	}

	t.Run("index lists every table with its bounds", func(t *testing.T) {
		rec := serve("/api/v1/mainnet/tables")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var body struct {
			Tables []tables.Table `json:"tables"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Tables, 2)

		assert.Equal(t, "dim_node", body.Tables[0].Name)
		assert.Nil(t, body.Tables[0].Bounds)

		assert.Equal(t, "fct_block_head", body.Tables[1].Name)
		assert.Equal(t, "/api/v1/mainnet/fct_block_head", body.Tables[1].Path)
		assert.Equal(t, &bounds.TableBounds{Min: 100, Max: 200}, body.Tables[1].Bounds)
	})

	t.Run("detail describes the table's fields", func(t *testing.T) {
		rec := serve("/api/v1/mainnet/tables/fct_block_head")
		require.Equal(t, http.StatusOK, rec.Code)

		var table tables.TableDetail
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &table))
		assert.Equal(t, "Block details for the unfinalized chain", table.Description)
		assert.Equal(t, []string{"slot_start_date_time", "slot"}, table.PrimaryKey)
		assert.Equal(t, &bounds.TableBounds{Min: 100, Max: 200}, table.Bounds)
		assert.Len(t, table.Fields, 5)
	})

	t.Run("spec is fetched once and cached", func(t *testing.T) {
		assert.Equal(t, 1, specFetches)
	})

	t.Run("unknown table is 404", func(t *testing.T) {
		rec := serve("/api/v1/mainnet/tables/nope")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.JSONEq(t, `{"error":"table not found","table":"nope"}`, rec.Body.String())
	})

	t.Run("unknown network is 404", func(t *testing.T) {
		rec := serve("/api/v1/nope/tables")
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.JSONEq(t, `{"error":"network not found","network":"nope"}`, rec.Body.String())
	})

	t.Run("unreachable backend is 502", func(t *testing.T) {
		provider.EXPECT().GetBounds(gomock.Any(), "broken").Return(nil, false).AnyTimes()

		rec := serve("/api/v1/broken/tables")
		assert.Equal(t, http.StatusBadGateway, rec.Code)
	})

	t.Run("other API paths go to the proxy", func(t *testing.T) {
		assert.Equal(t, http.StatusTeapot, serve("/api/v1/mainnet/fct_block_head?slot_eq=1").Code)
		assert.Equal(t, http.StatusTeapot, serve("/api/v1/mainnet/openapi.yaml").Code)
	})
}
