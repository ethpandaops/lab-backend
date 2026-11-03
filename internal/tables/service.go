package tables

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// refreshAfter is how old a catalog gets before a request triggers a background refetch of the spec.
const refreshAfter = 10 * time.Minute

// ErrUnknownNetwork is returned for a network the proxy does not serve.
var ErrUnknownNetwork = errors.New("network not found")

// Service keeps a catalog per network, parsed from the OpenAPI spec of the network's backend. A network's first
// request fetches the spec; later requests get the cached catalog and, once it is older than refreshAfter, refetch it
// in the background.
type Service struct {
	targetURL func(network string) (string, bool)
	client    *http.Client
	logger    logrus.FieldLogger

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	load       sync.Mutex // held while a request fetches the network's first catalog
	current    atomic.Pointer[loaded]
	refreshing atomic.Bool
}

type loaded struct {
	catalog *Catalog
	at      time.Time
}

// NewService creates a Service. targetURL returns a network's backend URL, and false for an unknown network.
func NewService(targetURL func(network string) (string, bool), logger logrus.FieldLogger) *Service {
	return &Service{
		targetURL: targetURL,
		client:    &http.Client{Timeout: time.Minute},
		logger:    logger.WithField("component", "tables"),
		entries:   make(map[string]*entry),
	}
}

// Catalog returns the network's catalog.
func (s *Service) Catalog(ctx context.Context, network string) (*Catalog, error) {
	target, ok := s.targetURL(network)
	if !ok {
		return nil, ErrUnknownNetwork
	}

	s.mu.Lock()

	e, ok := s.entries[network]
	if !ok {
		e = &entry{}
		s.entries[network] = e
	}

	s.mu.Unlock()

	if cur := e.current.Load(); cur != nil {
		if time.Since(cur.at) > refreshAfter && e.refreshing.CompareAndSwap(false, true) {
			// The refresh outlives the request that triggered it.
			refreshCtx := context.WithoutCancel(ctx)

			go func() {
				defer e.refreshing.Store(false)

				if _, err := s.fetch(refreshCtx, e, network, target); err != nil {
					s.logger.WithError(err).WithField("network", network).Warn("Failed to refresh table catalog")
				}
			}()
		}

		return cur.catalog, nil
	}

	e.load.Lock()
	defer e.load.Unlock()

	if cur := e.current.Load(); cur != nil {
		return cur.catalog, nil
	}

	return s.fetch(ctx, e, network, target)
}

func (s *Service) fetch(ctx context.Context, e *entry, network, target string) (*Catalog, error) {
	specURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse backend url: %w", err)
	}

	// The backend serves its spec at /openapi.yaml whatever path the target URL has, as the proxy assumes.
	specURL.Path = "/openapi.yaml"
	specURL.RawQuery = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build spec request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch spec: backend returned %s", resp.Status)
	}

	spec, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}

	catalog, err := Parse(network, spec)
	if err != nil {
		return nil, err
	}

	e.current.Store(&loaded{catalog: catalog, at: time.Now()})

	s.logger.WithFields(logrus.Fields{
		"network": network,
		"tables":  len(catalog.tables),
	}).Info("Loaded table catalog")

	return catalog, nil
}
