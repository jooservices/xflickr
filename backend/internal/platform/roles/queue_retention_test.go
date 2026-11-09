package roles

import (
	"context"
	"errors"
	"testing"
	"time"

	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

func TestRiverRetentionUsesEffectiveApplicationSettings(t *testing.T) {
	reader := &effectiveQueueSettingsFake{value: settingsdomain.Effective{
		QueueCompletedRetention: 2 * time.Hour,
		QueueDiscardedRetention: 72 * time.Hour,
	}}

	got, err := riverRetention(t.Context(), reader)
	if err != nil {
		t.Fatal(err)
	}
	if got.Completed != reader.value.QueueCompletedRetention || got.Discarded != reader.value.QueueDiscardedRetention {
		t.Fatalf("riverRetention() = %+v, want effective application values", got)
	}
	if reader.userID != 0 {
		t.Fatalf("riverRetention() loaded user %d, want global settings user 0", reader.userID)
	}
}

func TestRiverRetentionPropagatesSettingsErrors(t *testing.T) {
	reader := &effectiveQueueSettingsFake{err: errors.New("settings unavailable")}
	if _, err := riverRetention(t.Context(), reader); err == nil {
		t.Fatal("riverRetention() ignored settings error")
	}
}

func TestRiverRetentionRequiresSettingsReader(t *testing.T) {
	if _, err := riverRetention(t.Context(), nil); err == nil {
		t.Fatal("riverRetention() accepted a nil settings reader")
	}
}

type effectiveQueueSettingsFake struct {
	value  settingsdomain.Effective
	userID int64
	err    error
}

func (reader *effectiveQueueSettingsFake) Effective(_ context.Context, userID int64) (settingsdomain.Effective, error) {
	reader.userID = userID
	return reader.value, reader.err
}
