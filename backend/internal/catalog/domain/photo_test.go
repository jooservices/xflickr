package domain

import (
	"errors"
	"testing"
	"time"
)

func TestCanReplaceSizes(t *testing.T) {
	for _, test := range []struct {
		current, incoming SizesStatus
		want              bool
	}{
		{SizesNone, SizesProvisional, true},
		{SizesProvisional, SizesConfirmed, true},
		{SizesConfirmed, SizesProvisional, false},
		{SizesConfirmed, SizesConfirmed, true},
	} {
		if got := CanReplaceSizes(test.current, test.incoming); got != test.want {
			t.Errorf("CanReplaceSizes(%q, %q) = %v, want %v", test.current, test.incoming, got, test.want)
		}
	}
}

func TestPhotoValidate(t *testing.T) {
	valid := Photo{ID: 1, OwnerNSID: "person@example", LastUpdate: time.Now(), Visibility: VisibilityPublic, Raw: []byte(`{}`)}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid photo rejected: %v", err)
	}
	invalid := []Photo{
		{},
		{ID: 1, OwnerNSID: "person", LastUpdate: time.Now(), Visibility: "unknown", Raw: []byte(`{}`)},
		{ID: 1, OwnerNSID: "person", LastUpdate: time.Now(), Visibility: VisibilityPublic, SizesStatus: "unknown", Raw: []byte(`{}`)},
	}
	for _, photo := range invalid {
		if err := photo.Validate(); !errors.Is(err, ErrInvalidPhoto) {
			t.Errorf("Validate() error = %v, want ErrInvalidPhoto", err)
		}
	}
}
