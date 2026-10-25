package app

import (
	"context"
	"errors"
)

var ErrTempStorageFull = errors.New("media: temporary storage is full")

// StoragePressure reports whether temporary media storage should accept work.
type StoragePressure interface {
	Full(context.Context) (bool, error)
}

// Pressure centralizes the media back-pressure check used by downloads and
// the scheduler. BlobStore.Full includes its configured capacity and, for a
// local store, the underlying filesystem's free-space threshold.
type Pressure struct {
	Store StoragePressure
}

func (p Pressure) Check(ctx context.Context) error {
	if ctx == nil || p.Store == nil {
		return errors.New("media: storage pressure dependencies are required")
	}
	full, err := p.Store.Full(ctx)
	if err != nil {
		return err
	}
	if full {
		return ErrTempStorageFull
	}
	return nil
}

func (p Pressure) Full(ctx context.Context) (bool, error) {
	err := p.Check(ctx)
	if errors.Is(err, ErrTempStorageFull) {
		return true, nil
	}
	return false, err
}
