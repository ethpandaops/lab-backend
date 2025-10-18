package frontend

import (
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHeadData(t *testing.T) {
	t.Run("loads valid head.json", func(t *testing.T) {
		headJSON := `{
			"_default": {
				"meta": [{"name": "viewport", "content": "width=device-width"}],
				"raw": "<meta name=\"viewport\" content=\"width=device-width\">"
			},
			"/about": {
				"meta": [{"title": "About"}],
				"raw": "<title>About</title>"
			},
			"/": {
				"meta": [{"title": "Home"}],
				"raw": "<title>Home</title>"
			}
		}`

		fs := fstest.MapFS{
			"head.json": &fstest.MapFile{
				Data: []byte(headJSON),
			},
		}

		headData, err := LoadHeadData(fs)
		require.NoError(t, err)
		assert.Len(t, headData, 3)

		// Check _default exists
		defaultHead := headData.GetRouteHead("_default")
		require.NotNil(t, defaultHead)
		assert.Contains(t, defaultHead.Raw, "viewport")

		// Check /about exists
		aboutHead := headData.GetRouteHead("/about")
		require.NotNil(t, aboutHead)
		assert.Contains(t, aboutHead.Raw, "About")

		// Check / exists
		homeHead := headData.GetRouteHead("/")
		require.NotNil(t, homeHead)
		assert.Contains(t, homeHead.Raw, "Home")
	})

	t.Run("returns empty HeadData when head.json missing", func(t *testing.T) {
		fs := fstest.MapFS{}

		headData, err := LoadHeadData(fs)
		require.NoError(t, err)
		assert.Empty(t, headData)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		fs := fstest.MapFS{
			"head.json": &fstest.MapFile{
				Data: []byte("invalid json"),
			},
		}

		_, err := LoadHeadData(fs)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse head.json")
	})
}

func TestHeadData_GetRouteHead(t *testing.T) {
	headData := HeadData{
		"_default": RouteHead{
			Raw: "<meta name=\"default\">",
		},
		"/about": RouteHead{
			Raw: "<title>About</title>",
		},
		"/": RouteHead{
			Raw: "<title>Home</title>",
		},
	}

	t.Run("returns exact match", func(t *testing.T) {
		aboutHead := headData.GetRouteHead("/about")
		require.NotNil(t, aboutHead)
		assert.Equal(t, "<title>About</title>", aboutHead.Raw)
	})

	t.Run("normalizes empty string to /", func(t *testing.T) {
		homeHead := headData.GetRouteHead("")
		require.NotNil(t, homeHead)
		assert.Equal(t, "<title>Home</title>", homeHead.Raw)
	})

	t.Run("normalizes index.html to /", func(t *testing.T) {
		homeHead := headData.GetRouteHead(indexFileName)
		require.NotNil(t, homeHead)
		assert.Equal(t, "<title>Home</title>", homeHead.Raw)
	})

	t.Run("falls back to _default for unknown route", func(t *testing.T) {
		unknownHead := headData.GetRouteHead("/unknown")
		require.NotNil(t, unknownHead)
		assert.Equal(t, "<meta name=\"default\">", unknownHead.Raw)
	})

	t.Run("returns nil when no match and no _default", func(t *testing.T) {
		data := HeadData{
			"/about": RouteHead{Raw: "<title>About</title>"},
		}

		unknownHead := data.GetRouteHead("/unknown")
		assert.Nil(t, unknownHead)
	})
}

func TestHeadData_GetAllRoutes(t *testing.T) {
	headData := HeadData{
		"_default": RouteHead{Raw: "default"},
		"/about":   RouteHead{Raw: "about"},
		"/":        RouteHead{Raw: "home"},
	}

	routes := headData.GetAllRoutes()
	assert.Len(t, routes, 3)
	assert.Contains(t, routes, "_default")
	assert.Contains(t, routes, "/about")
	assert.Contains(t, routes, "/")
}
