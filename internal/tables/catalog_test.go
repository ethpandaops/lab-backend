package tables

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	spec, err := os.ReadFile("testdata/openapi.yaml")
	require.NoError(t, err)

	catalog, err := Parse("mainnet", spec)
	require.NoError(t, err)

	// The get-by-key path is not a table, and tables are sorted by name.
	assert.Equal(t, []Table{
		{Name: "dim_node", Path: "/api/v1/mainnet/dim_node", PrimaryKey: []string{"name"}},
		{
			Name:        "fct_block_head",
			Description: "Block details for the unfinalized chain",
			Path:        "/api/v1/mainnet/fct_block_head",
			PrimaryKey:  []string{"slot_start_date_time", "slot"},
		},
	}, catalog.Tables())

	table, ok := catalog.Table("fct_block_head")
	require.True(t, ok)

	// slot_start_date_time_* parameters belong to slot_start_date_time, not slot; parameters that filter no field
	// are listed on their own.
	assert.Equal(t, []Field{
		{Name: "block_root", Type: "string", Description: "The root hash of the beacon block", Filters: []string{"like"}},
		{Name: "block_total_bytes", Type: "uint32", Nullable: true, Description: "The total bytes of the beacon block payload"},
		{Name: "slot", Type: "uint32", Description: "The slot number from beacon block payload", Filters: []string{"eq", "in_values"}},
		{Name: "slot_start_date_time", Type: "uint32", Description: "The wall clock time when the slot started", Filters: []string{"eq", "gte"}},
		{Name: "versioned_hashes", Type: "string[]", Description: "The versioned hashes"},
	}, table.Fields)
	assert.Equal(t, []Parameter{
		{Name: "page_size", Type: "int32", Description: "The maximum number of fct_block_head to return."},
		{Name: "page_token", Type: "string"},
	}, table.Parameters)

	_, ok = catalog.Table("fct_block_head_local")
	assert.False(t, ok)
}
