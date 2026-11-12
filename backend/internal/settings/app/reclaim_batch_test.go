package app

import (
	"strconv"
	"testing"
)

func TestValidateReclaimBatchSize(t *testing.T) {
	for _, value := range []int{-1, 0, 1, 100, 10000, 10001} {
		t.Run(strconv.Itoa(value), func(t *testing.T) {
			err := ValidateReclaimBatchSize(value)
			if wantErr := value < 1 || value > 10000; (err != nil) != wantErr {
				t.Fatalf("ValidateReclaimBatchSize(%d) = %v; want error=%v", value, err, wantErr)
			}
		})
	}
}
