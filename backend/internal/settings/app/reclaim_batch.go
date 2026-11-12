package app

import (
	"encoding/json"
	"strconv"

	"github.com/jooservices/xflickr/internal/settings/domain"
)

// ValidateReclaimBatchSize checks the scheduler batch against the settings registry.
func ValidateReclaimBatchSize(value int) error {
	return domain.ValidateValue(domain.QueueReclaimBatchSize, json.RawMessage(strconv.Itoa(value)), nil, nil)
}
