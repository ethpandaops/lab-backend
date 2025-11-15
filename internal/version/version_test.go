package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet(t *testing.T) {
	// Test basic Get without frontend version
	info := Get()

	assert.NotEmpty(t, info.Version)
	assert.NotEmpty(t, info.GitCommit)
	assert.NotEmpty(t, info.BuildDate)
	assert.Empty(t, info.FrontendVersion)
}

func TestGetWithFrontend(t *testing.T) {
	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)

	defer func() {
		// Restore working directory
		require.NoError(t, os.Chdir(originalWd))
	}()

	// Create temporary directory structure
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	t.Run("reads frontend version when file exists", func(t *testing.T) {
		// Create .tmp directory and frontend version file
		require.NoError(t, os.MkdirAll(".tmp", 0o755))

		frontendVersion := "frontend-v2.5.0-test"
		require.NoError(t, os.WriteFile(".tmp/frontend-version.txt", []byte(frontendVersion), 0o644))

		info := GetWithFrontend()

		assert.NotEmpty(t, info.Version)
		assert.NotEmpty(t, info.GitCommit)
		assert.NotEmpty(t, info.BuildDate)
		assert.Equal(t, frontendVersion, info.FrontendVersion)

		// Cleanup
		require.NoError(t, os.RemoveAll(".tmp"))
	})

	t.Run("handles missing frontend version file", func(t *testing.T) {
		// Ensure .tmp directory doesn't exist
		os.RemoveAll(".tmp")

		info := GetWithFrontend()

		assert.NotEmpty(t, info.Version)
		assert.NotEmpty(t, info.GitCommit)
		assert.NotEmpty(t, info.BuildDate)
		assert.Empty(t, info.FrontendVersion)
	})

	t.Run("trims whitespace from frontend version", func(t *testing.T) {
		// Create .tmp directory and frontend version file with whitespace
		require.NoError(t, os.MkdirAll(".tmp", 0o755))

		frontendVersion := "  frontend-v3.0.0  \n"
		require.NoError(t, os.WriteFile(".tmp/frontend-version.txt", []byte(frontendVersion), 0o644))

		info := GetWithFrontend()

		assert.Equal(t, "frontend-v3.0.0", info.FrontendVersion)

		// Cleanup
		require.NoError(t, os.RemoveAll(".tmp"))
	})
}

func TestReadFrontendVersion(t *testing.T) {
	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)

	defer func() {
		// Restore working directory
		require.NoError(t, os.Chdir(originalWd))
	}()

	// Create temporary directory structure
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	t.Run("returns empty string when file doesn't exist", func(t *testing.T) {
		version := readFrontendVersion()
		assert.Empty(t, version)
	})

	t.Run("reads and trims version from file", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(".tmp", 0o755))
		require.NoError(t, os.WriteFile(".tmp/frontend-version.txt", []byte("  v1.2.3  \n"), 0o644))

		version := readFrontendVersion()
		assert.Equal(t, "v1.2.3", version)

		// Cleanup
		require.NoError(t, os.RemoveAll(".tmp"))
	})

	t.Run("handles empty file", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(".tmp", 0o755))
		require.NoError(t, os.WriteFile(".tmp/frontend-version.txt", []byte(""), 0o644))

		version := readFrontendVersion()
		assert.Empty(t, version)

		// Cleanup
		require.NoError(t, os.RemoveAll(".tmp"))
	})
}

func TestShort(t *testing.T) {
	// Just verify it returns the version string
	result := Short()
	assert.Equal(t, Version, result)
}

func TestFull(t *testing.T) {
	// Verify it returns formatted string with all fields
	result := Full()
	assert.Contains(t, result, Version)
	assert.Contains(t, result, GitCommit)
	assert.Contains(t, result, BuildDate)
	assert.Contains(t, result, "commit:")
	assert.Contains(t, result, "built:")
}

func TestInfo_JSONMarshaling(t *testing.T) {
	// Save original working directory
	originalWd, err := os.Getwd()
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.Chdir(originalWd))
	}()

	// Create temporary directory
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))

	t.Run("omits frontend_version when empty", func(t *testing.T) {
		info := Get()
		data, err := json.Marshal(info)
		require.NoError(t, err)

		// Check that frontend_version is not in the JSON when empty
		assert.NotContains(t, string(data), "frontend_version")
	})

	t.Run("includes frontend_version when present", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(".tmp", 0o755))
		require.NoError(t, os.WriteFile(
			filepath.Join(".tmp", "frontend-version.txt"),
			[]byte("frontend-v1.0.0"),
			0o644,
		))

		info := GetWithFrontend()
		data, err := json.Marshal(info)
		require.NoError(t, err)

		// Check that frontend_version is in the JSON
		assert.Contains(t, string(data), "frontend_version")
		assert.Contains(t, string(data), "frontend-v1.0.0")

		// Cleanup
		require.NoError(t, os.RemoveAll(".tmp"))
	})
}
