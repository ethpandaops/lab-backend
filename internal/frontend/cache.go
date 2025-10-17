package frontend

import (
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/sirupsen/logrus"
)

// IndexCache caches index.html in memory with injected config.
// Thread-safe for concurrent reads.
type IndexCache struct {
	mu       sync.RWMutex
	original []byte // Original index.html
	injected []byte // Config-injected version
}

// Prewarm loads index.html into memory and injects config and bounds.
// Called once during initialization.
func (ic *IndexCache) Prewarm(
	logger logrus.FieldLogger,
	filesystem fs.FS,
	configData interface{},
	boundsData interface{},
) error {
	// Open index.html
	file, err := filesystem.Open("index.html")
	if err != nil {
		return fmt.Errorf("failed to open index.html: %w", err)
	}
	defer file.Close()

	// Read entire file into memory
	original, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read index.html: %w", err)
	}

	logger.WithField("size", len(original)).Info("Loaded index.html into memory")

	// Inject config and bounds in a single script tag
	injected, err := InjectConfigAndBounds(original, configData, boundsData)
	if err != nil {
		return fmt.Errorf("failed to inject config and bounds: %w", err)
	}

	logger.WithField("size", len(injected)).Info("Config and bounds injected into index.html")

	// Store in cache
	ic.mu.Lock()
	defer ic.mu.Unlock()

	ic.original = original
	ic.injected = injected

	return nil
}

// GetInjected returns the cached config-injected index.html.
// Thread-safe for concurrent reads.
func (ic *IndexCache) GetInjected() []byte {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	return ic.injected
}

// GetOriginal returns the cached original index.html.
// Thread-safe for concurrent reads.
func (ic *IndexCache) GetOriginal() []byte {
	ic.mu.RLock()
	defer ic.mu.RUnlock()

	return ic.original
}

// Update updates the injected config and bounds (for future dynamic updates).
// Thread-safe for concurrent access.
func (ic *IndexCache) Update(
	configData interface{},
	boundsData interface{},
) error {
	ic.mu.RLock()
	original := ic.original
	ic.mu.RUnlock()

	// Inject new config and bounds in a single script tag
	injected, err := InjectConfigAndBounds(original, configData, boundsData)
	if err != nil {
		return fmt.Errorf("failed to inject config and bounds: %w", err)
	}

	// Update cache
	ic.mu.Lock()
	defer ic.mu.Unlock()

	ic.injected = injected

	return nil
}
