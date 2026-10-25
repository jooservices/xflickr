// Package settings stores the crawl depth through the settings application service.
package settings

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jooservices/xflickr/internal/crawl/app"
	settingsapp "github.com/jooservices/xflickr/internal/settings/app"
	settingsdomain "github.com/jooservices/xflickr/internal/settings/domain"
)

// Service is the settings application API this adapter needs.
type Service interface {
	DepthPolicy(context.Context, int64) (settingsapp.DepthPolicy, error)
	SetUser(context.Context, int64, settingsdomain.Key, json.RawMessage) error
}

// Depth implements app.DepthStore on the max_depth user setting.
type Depth struct {
	Settings Service
}

func (d Depth) Policy(ctx context.Context, userID int64) (app.DepthPolicy, error) {
	if d.Settings == nil {
		return app.DepthPolicy{}, errors.New("crawl: settings service is required")
	}
	policy, err := d.Settings.DepthPolicy(ctx, userID)
	if err != nil {
		return app.DepthPolicy{}, err
	}
	return app.DepthPolicy{Depth: policy.Depth, Default: policy.Default, Max: policy.Max}, nil
}

func (d Depth) SetDepth(ctx context.Context, userID int64, depth int) error {
	if d.Settings == nil {
		return errors.New("crawl: settings service is required")
	}
	err := d.Settings.SetUser(ctx, userID, settingsdomain.MaxDepth, json.RawMessage(strconv.Itoa(depth)))
	var validation *settingsdomain.ValidationError
	if errors.As(err, &validation) {
		return app.ErrDepthAboveCap
	}
	return err
}
