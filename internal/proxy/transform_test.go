package proxy

import (
	"testing"
	"time"

	"github.com/ethpandaops/lab-backend/internal/wallclock"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestWallclock(t *testing.T) *wallclock.Service {
	t.Helper()

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce noise in tests

	svc := wallclock.New(logger)

	// Add mainnet with genesis time: Dec 1, 2020, 12:00:23 UTC
	genesisTime := time.Unix(1606824023, 0)
	err := svc.AddNetwork(wallclock.NetworkConfig{
		Name:           "mainnet",
		GenesisTime:    genesisTime,
		SecondsPerSlot: 12,
	})
	require.NoError(t, err)

	return svc
}

func TestDetectSlotFilter(t *testing.T) {
	tests := []struct {
		name           string
		key            string
		values         []string
		expectedIsSlot bool
		expectedOp     string
		expectedValue  uint64
	}{
		{
			name:           "slot_eq",
			key:            "slot_eq",
			values:         []string{"1000"},
			expectedIsSlot: true,
			expectedOp:     "eq",
			expectedValue:  1000,
		},
		{
			name:           "slot_gte",
			key:            "slot_gte",
			values:         []string{"2000"},
			expectedIsSlot: true,
			expectedOp:     "gte",
			expectedValue:  2000,
		},
		{
			name:           "slot_lte",
			key:            "slot_lte",
			values:         []string{"3000"},
			expectedIsSlot: true,
			expectedOp:     "lte",
			expectedValue:  3000,
		},
		{
			name:           "slot_gt",
			key:            "slot_gt",
			values:         []string{"4000"},
			expectedIsSlot: true,
			expectedOp:     "gt",
			expectedValue:  4000,
		},
		{
			name:           "slot_lt",
			key:            "slot_lt",
			values:         []string{"5000"},
			expectedIsSlot: true,
			expectedOp:     "lt",
			expectedValue:  5000,
		},
		{
			name:           "non-slot parameter",
			key:            "limit",
			values:         []string{"100"},
			expectedIsSlot: false,
			expectedOp:     "",
			expectedValue:  0,
		},
		{
			name:           "slot with unknown operator",
			key:            "slot_unknown",
			values:         []string{"1000"},
			expectedIsSlot: false,
			expectedOp:     "",
			expectedValue:  0,
		},
		{
			name:           "slot with invalid value",
			key:            "slot_eq",
			values:         []string{"invalid"},
			expectedIsSlot: false,
			expectedOp:     "",
			expectedValue:  0,
		},
		{
			name:           "slot with no values",
			key:            "slot_eq",
			values:         []string{},
			expectedIsSlot: false,
			expectedOp:     "",
			expectedValue:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isSlot, op, value := detectSlotFilter(tt.key, tt.values)

			assert.Equal(t, tt.expectedIsSlot, isSlot)
			assert.Equal(t, tt.expectedOp, op)
			assert.Equal(t, tt.expectedValue, value)
		})
	}
}

const testSlotEq1000 = "slot_eq=1000"

func TestTransformQueryParams_SingleSlotFilter(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	// slot 1000 on mainnet should map to timestamp 1606836023
	originalQuery := testSlotEq1000
	transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

	// Should transform to slot_start_date_time_eq
	assert.Contains(t, transformed, "slot_start_date_time_eq=1606836023")
	assert.NotContains(t, transformed, "slot_eq")
}

func TestTransformQueryParams_MultipleSlotFilters(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	// slot 1000 -> 1606836023, slot 2000 -> 1606848023
	originalQuery := "slot_gte=1000&slot_lte=2000"
	transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

	// Should transform both filters
	assert.Contains(t, transformed, "slot_start_date_time_gte=1606836023")
	assert.Contains(t, transformed, "slot_start_date_time_lte=1606848023")
	assert.NotContains(t, transformed, "slot_gte")
	assert.NotContains(t, transformed, "slot_lte")
}

func TestTransformQueryParams_MixedFilters(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	originalQuery := "slot_eq=1000&limit=100&offset=0"
	transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

	// Should transform slot filter but preserve other params
	assert.Contains(t, transformed, "slot_start_date_time_eq=1606836023")
	assert.Contains(t, transformed, "limit=100")
	assert.Contains(t, transformed, "offset=0")
	assert.NotContains(t, transformed, "slot_eq")
}

func TestTransformQueryParams_NoSlotFilters(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	originalQuery := "limit=100&offset=0"
	transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

	// Should return original query unchanged
	assert.Equal(t, originalQuery, transformed)
}

func TestTransformQueryParams_WallclockUnavailable(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	// Query for network that doesn't exist
	originalQuery := testSlotEq1000
	transformed := transformQueryParams(logger, "nonexistent", svc, originalQuery)

	// Should return original query (fail-open)
	assert.Equal(t, originalQuery, transformed)
}

func TestTransformQueryParams_NilWallclockService(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	originalQuery := testSlotEq1000
	transformed := transformQueryParams(logger, "mainnet", nil, originalQuery)

	// Should return original query (fail-open)
	assert.Equal(t, originalQuery, transformed)
}

func TestTransformQueryParams_EmptyQuery(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	originalQuery := ""
	transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

	// Should return empty string
	assert.Equal(t, "", transformed)
}

func TestTransformQueryParams_InvalidQuery(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	// Invalid query string (malformed)
	originalQuery := "slot_eq=1000&invalid%%query"
	transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

	// Should return original query on parse failure (fail-open)
	assert.Equal(t, originalQuery, transformed)
}

func TestTransformQueryParams_AllOperators(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	svc := setupTestWallclock(t)

	tests := []struct {
		name             string
		operator         string
		slot             uint64
		expectedOperator string
		expectedTime     uint32
	}{
		{
			name:             "eq",
			operator:         "eq",
			slot:             1000,
			expectedOperator: "eq",
			expectedTime:     1606836023,
		},
		{
			name:             "gte",
			operator:         "gte",
			slot:             1000,
			expectedOperator: "gte",
			expectedTime:     1606836023,
		},
		{
			name:             "lte",
			operator:         "lte",
			slot:             1000,
			expectedOperator: "lte",
			expectedTime:     1606836023,
		},
		{
			name:             "gt",
			operator:         "gt",
			slot:             1000,
			expectedOperator: "gt",
			expectedTime:     1606836023,
		},
		{
			name:             "lt",
			operator:         "lt",
			slot:             1000,
			expectedOperator: "lt",
			expectedTime:     1606836023,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalQuery := "slot_" + tt.operator + "=1000"
			transformed := transformQueryParams(logger, "mainnet", svc, originalQuery)

			expectedKey := "slot_start_date_time_" + tt.expectedOperator
			expectedValue := "1606836023"

			assert.Contains(t, transformed, expectedKey+"="+expectedValue)
			assert.NotContains(t, transformed, "slot_"+tt.operator)
		})
	}
}
