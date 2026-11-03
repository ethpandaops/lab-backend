package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/lab-backend/internal/bounds"
	"github.com/ethpandaops/lab-backend/internal/tables"
)

// Verify interface compliance at compile time.
var _ http.Handler = (*TablesHandler)(nil)

// TablesHandler serves GET /api/v1/{network}/tables, an index of every table the network's API serves, and
// GET /api/v1/{network}/tables/{table}, one table's fields and filters. Both are built from the network's OpenAPI spec.
//
// Their patterns would conflict with the download and gas profiler routes in the server's mux, so the handler sits
// in front of the API proxy and passes every other request to it.
type TablesHandler struct {
	catalogs *tables.Service
	bounds   bounds.Provider
	logger   logrus.FieldLogger
	mux      *http.ServeMux
}

// NewTablesHandler creates a TablesHandler that passes requests it does not serve to next.
func NewTablesHandler(
	catalogs *tables.Service,
	boundsProvider bounds.Provider,
	next http.Handler,
	logger logrus.FieldLogger,
) *TablesHandler {
	h := &TablesHandler{
		catalogs: catalogs,
		bounds:   boundsProvider,
		logger:   logger.WithField("handler", "tables"),
		mux:      http.NewServeMux(),
	}

	h.mux.HandleFunc("GET /api/v1/{network}/tables", h.list)
	h.mux.HandleFunc("GET /api/v1/{network}/tables/{table}", h.get)
	h.mux.Handle("/", next)

	return h
}

// ServeHTTP routes the request.
func (h *TablesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *TablesHandler) list(w http.ResponseWriter, r *http.Request) {
	network := r.PathValue("network")

	catalog, ok := h.catalog(w, r, network)
	if !ok {
		return
	}

	all := catalog.Tables()
	tableBounds := h.tableBounds(r, network)

	for i := range all {
		all[i].Bounds = tableBounds[all[i].Name]
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"tables": all})
}

func (h *TablesHandler) get(w http.ResponseWriter, r *http.Request) {
	network := r.PathValue("network")
	name := r.PathValue("table")

	catalog, ok := h.catalog(w, r, network)
	if !ok {
		return
	}

	table, ok := catalog.Table(name)
	if !ok {
		h.writeJSON(w, http.StatusNotFound, map[string]string{"error": "table not found", "table": name})

		return
	}

	table.Bounds = h.tableBounds(r, network)[name]

	h.writeJSON(w, http.StatusOK, table)
}

// catalog returns the network's catalog, or writes the error response and returns false.
func (h *TablesHandler) catalog(w http.ResponseWriter, r *http.Request, network string) (*tables.Catalog, bool) {
	catalog, err := h.catalogs.Catalog(r.Context(), network)
	if err == nil {
		return catalog, true
	}

	if errors.Is(err, tables.ErrUnknownNetwork) {
		h.writeJSON(w, http.StatusNotFound, map[string]string{"error": "network not found", "network": network})

		return nil, false
	}

	h.logger.WithError(err).WithField("network", network).Error("Failed to load table catalog")
	h.writeJSON(w, http.StatusBadGateway, map[string]string{"error": "backend unavailable", "network": network})

	return nil, false
}

// tableBounds returns each table's bounds, or nil when the network has none.
func (h *TablesHandler) tableBounds(r *http.Request, network string) map[string]*bounds.TableBounds {
	if h.bounds == nil {
		return nil
	}

	data, ok := h.bounds.GetBounds(r.Context(), network)
	if !ok {
		return nil
	}

	out := make(map[string]*bounds.TableBounds, len(data.Tables))

	for name, b := range data.Tables {
		out[name] = &b
	}

	return out
}

func (h *TablesHandler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.WithError(err).Error("Failed to encode response")
	}
}
