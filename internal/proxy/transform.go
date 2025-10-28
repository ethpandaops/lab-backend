package proxy

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/ethpandaops/lab-backend/internal/wallclock"
	"github.com/sirupsen/logrus"
)

// transformQueryParams transforms slot_* filters to slot_start_date_time_* filters.
// Returns the original query string if transformation fails (fail-open).
func transformQueryParams(
	logger logrus.FieldLogger,
	networkName string,
	wallclockSvc *wallclock.Service,
	originalQuery string,
) string {
	// If no wallclock service or empty query, return original
	if wallclockSvc == nil || originalQuery == "" {
		return originalQuery
	}

	// Parse query string
	values, err := url.ParseQuery(originalQuery)
	if err != nil {
		logger.WithFields(logrus.Fields{
			"network": networkName,
			"query":   originalQuery,
			"error":   err.Error(),
		}).Warn("Failed to parse query string, using original")

		return originalQuery
	}

	// Track if any transformations were made
	transformed := false
	transformedValues := make(url.Values)

	// Iterate over each parameter
	for key, valuesSlice := range values {
		// Check if this is a slot filter
		isSlot, operator, slotValue := detectSlotFilter(key, valuesSlice)

		if !isSlot {
			// Not a slot filter, copy as-is
			transformedValues[key] = valuesSlice

			continue
		}

		// Calculate slot_start_date_time
		slotStartTime := wallclockSvc.CalculateSlotStartTime(networkName, slotValue)

		if slotStartTime == 0 {
			// Wallclock unavailable or calculation failed, keep original slot filter
			logger.WithFields(logrus.Fields{
				"network": networkName,
				"slot":    slotValue,
			}).Warn("Failed to calculate slot start time, using original slot filter")

			transformedValues[key] = valuesSlice

			continue
		}

		// Replace slot_* with slot_start_date_time_*
		newKey := "slot_start_date_time_" + operator
		transformedValues[newKey] = []string{strconv.FormatUint(uint64(slotStartTime), 10)}
		transformed = true

		logger.WithFields(logrus.Fields{
			"network":              networkName,
			"slot":                 slotValue,
			"slot_start_date_time": slotStartTime,
			"operator":             operator,
		}).Debug("Transformed slot filter to slot_start_date_time")
	}

	// If no transformations were made, return original
	if !transformed {
		return originalQuery
	}

	// Return transformed query string
	return transformedValues.Encode()
}

// detectSlotFilter checks if a query parameter is a slot filter.
// Returns: isSlotFilter, operator (e.g., "eq", "gte"), value.
func detectSlotFilter(key string, values []string) (bool, string, uint64) {
	// Check if key starts with "slot_"
	if !strings.HasPrefix(key, "slot_") {
		return false, "", 0
	}

	// If no values, not a valid filter
	if len(values) == 0 {
		return false, "", 0
	}

	// Extract operator from key
	operator := strings.TrimPrefix(key, "slot_")

	// Validate operator
	switch operator {
	case "eq", "gte", "lte", "gt", "lt":
		// Valid operator
	default:
		// Unknown operator
		return false, "", 0
	}

	// Parse slot value
	slotValue, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		// Invalid slot value
		return false, "", 0
	}

	return true, operator, slotValue
}
