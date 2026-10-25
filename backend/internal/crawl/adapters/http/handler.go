package http

import (
	"context"
	"errors"
	"strings"

	"github.com/jooservices/xflickr/internal/crawl/app"
	"github.com/jooservices/xflickr/internal/platform/api/gen"
)

type ControlService interface {
	Status(context.Context, int64) (app.CrawlStatus, error)
	Star(context.Context, int64, string, bool) error
	Select(context.Context, int64, string) error
	Pause(context.Context, int64, string) error
	Resume(context.Context, int64, string) error
}

type CrawlHandler struct {
	Controls ControlService
	ActorID  func(context.Context) (int64, bool)
}

func (h CrawlHandler) GetMeCrawl(ctx context.Context, _ gen.GetMeCrawlRequestObject) (gen.GetMeCrawlResponseObject, error) {
	user, ok := h.actor(ctx)
	if !ok {
		return gen.GetMeCrawl401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if h.Controls == nil {
		return nil, errors.New("crawl: controls service is required")
	}
	status, err := h.Controls.Status(ctx, user)
	if err != nil {
		return nil, err
	}
	out := gen.CrawlStatus{Depths: make([]gen.DepthProgress, 0, len(status.Depths)), Facets: make([]gen.FacetProgress, 0, len(status.Facets)), Lanes: make([]gen.LaneStatus, 0, len(status.Lanes))}
	for _, v := range status.Depths {
		out.Depths = append(out.Depths, gen.DepthProgress{Depth: v.Depth, Contacts: v.Contacts})
	}
	for _, v := range status.Facets {
		out.Facets = append(out.Facets, gen.FacetProgress{Facet: v.Facet, Total: v.Total, Done: v.Done, Active: v.Active, Failed: v.Failed})
	}
	for _, v := range status.Lanes {
		out.Lanes = append(out.Lanes, gen.LaneStatus{Lane: gen.LaneStatusLane(v.Lane), Pending: v.Pending, Running: v.Running, Waiting: v.Waiting, Paused: v.Paused})
	}
	return gen.GetMeCrawl200JSONResponse(out), nil
}

func (h CrawlHandler) StarMyContact(ctx context.Context, request gen.StarMyContactRequestObject) (gen.StarMyContactResponseObject, error) {
	if request.Body == nil {
		return gen.StarMyContact400JSONResponse(gen.Error{Code: "invalid_request", Message: "Starred value is required"}), nil
	}
	user, nsid, status, err := h.prepare(ctx, request.Nsid)
	if err != nil {
		return nil, err
	}
	switch status {
	case 401:
		return gen.StarMyContact401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	case 404:
		return gen.StarMyContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err = h.Controls.Star(ctx, user, nsid, request.Body.Starred); errors.Is(err, app.ErrPersonNotFound) {
		return gen.StarMyContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.StarMyContact204Response{}, nil
}

func (h CrawlHandler) SelectMyContact(ctx context.Context, request gen.SelectMyContactRequestObject) (gen.SelectMyContactResponseObject, error) {
	user, nsid, status, err := h.prepare(ctx, request.Nsid)
	if err != nil {
		return nil, err
	}
	if status == 401 {
		return gen.SelectMyContact401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if status == 404 {
		return gen.SelectMyContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err = h.Controls.Select(ctx, user, nsid); errors.Is(err, app.ErrPersonNotFound) {
		return gen.SelectMyContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.SelectMyContact204Response{}, nil
}

func (h CrawlHandler) PauseCrawlContact(ctx context.Context, request gen.PauseCrawlContactRequestObject) (gen.PauseCrawlContactResponseObject, error) {
	if request.Body == nil {
		return gen.PauseCrawlContact400JSONResponse(gen.Error{Code: "invalid_request", Message: "Contact NSID is required"}), nil
	}
	user, nsid, status, err := h.prepare(ctx, request.Body.Nsid)
	if err != nil {
		return nil, err
	}
	if status == 401 {
		return gen.PauseCrawlContact401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if status == 404 {
		return gen.PauseCrawlContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err = h.Controls.Pause(ctx, user, nsid); errors.Is(err, app.ErrPersonNotFound) {
		return gen.PauseCrawlContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.PauseCrawlContact204Response{}, nil
}

func (h CrawlHandler) ResumeCrawlContact(ctx context.Context, request gen.ResumeCrawlContactRequestObject) (gen.ResumeCrawlContactResponseObject, error) {
	if request.Body == nil {
		return gen.ResumeCrawlContact400JSONResponse(gen.Error{Code: "invalid_request", Message: "Contact NSID is required"}), nil
	}
	user, nsid, status, err := h.prepare(ctx, request.Body.Nsid)
	if err != nil {
		return nil, err
	}
	if status == 401 {
		return gen.ResumeCrawlContact401JSONResponse(gen.Error{Code: "unauthenticated", Message: "Authentication required"}), nil
	}
	if status == 404 {
		return gen.ResumeCrawlContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err = h.Controls.Resume(ctx, user, nsid); errors.Is(err, app.ErrPersonNotFound) {
		return gen.ResumeCrawlContact404JSONResponse(gen.Error{Code: "not_found", Message: "Contact not found"}), nil
	}
	if err != nil {
		return nil, err
	}
	return gen.ResumeCrawlContact204Response{}, nil
}

func (h CrawlHandler) prepare(ctx context.Context, person string) (int64, string, int, error) {
	user, ok := h.actor(ctx)
	if !ok {
		return 0, "", 401, nil
	}
	if h.Controls == nil {
		return 0, "", 0, errors.New("crawl: controls service is required")
	}
	person = strings.TrimSpace(person)
	if person == "" {
		return 0, "", 0, app.ErrInvalidPerson
	}
	return user, person, 0, nil
}

func (h CrawlHandler) actor(ctx context.Context) (int64, bool) {
	if h.ActorID == nil {
		return 0, false
	}
	return h.ActorID(ctx)
}
