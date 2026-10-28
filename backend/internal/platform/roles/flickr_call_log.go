package roles

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jooservices/xflickr/internal/platform/obs"
	quotaPostgres "github.com/jooservices/xflickr/internal/quota/adapters/postgres"
	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

type flickrCallLog struct{ recorder *quotaapp.CallRecorder }

func (l flickrCallLog) RecordCall(ctx context.Context, call flickrclient.CallRecord) {
	fields := map[string]any{
		"method": call.Method, "key_id": call.KeyID, "user_id": callLogIDValue(call.UserID),
		"account_id": callLogIDValue(call.AccountID), "flickr_account": call.FlickrAccount,
		"lane": call.Lane, "facet": call.Facet, "attempt": call.Attempt,
		"started_at": call.StartedAt.UTC().Format(time.RFC3339Nano),
		"latency_ms": call.Latency.Milliseconds(), "http_status": call.HTTPStatus,
		"flickr_stat": call.FlickrStat, "error_code": call.FlickrErrorCode,
		"error_message": call.ErrorMessage, "response_bytes": call.ResponseBytes,
		"node_id": call.NodeID, "request_id": call.RequestID, "outcome": call.Outcome,
	}
	obs.Event(ctx, "flickr.call", fields)
	if call.Outcome != "success" {
		slog.Default().WarnContext(ctx, "flickr call failed", "method", call.Method, "outcome", call.Outcome, "error_code", call.FlickrErrorCode, "request_id", call.RequestID)
	}
	if l.recorder == nil || call.KeyID <= 0 {
		return
	}

	var userID, accountID, errorCode *int64
	if call.UserID != nil {
		value := *call.UserID
		userID = &value
	}
	if call.AccountID != nil {
		value := *call.AccountID
		accountID = &value
	}
	if call.FlickrErrorCode > 0 {
		value := int64(call.FlickrErrorCode)
		errorCode = &value
	}
	var httpStatus *int
	if call.HTTPStatus > 0 {
		value := call.HTTPStatus
		httpStatus = &value
	}
	startedAt := call.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if !l.recorder.Record(quotaapp.CallRecord{
		At: startedAt, KeyID: call.KeyID, UserID: userID, AccountID: accountID, FlickrAccount: call.FlickrAccount,
		Node: call.NodeID, NodeID: call.NodeID, Method: call.Method, Status: call.Outcome, ErrorCode: errorCode,
		LatencyMS: call.Latency.Milliseconds(), Bytes: call.ResponseBytes, ResponseBytes: call.ResponseBytes,
		Lane: call.Lane, Facet: call.Facet, Attempt: call.Attempt, StartedAt: startedAt, HTTPStatus: httpStatus,
		FlickrStat: call.FlickrStat, FlickrErrorCode: errorCode, ErrorMessage: call.ErrorMessage,
		RequestID: call.RequestID, Outcome: call.Outcome,
	}) {
		slog.Default().WarnContext(ctx, "flickr call log queue full or unavailable", "request_id", call.RequestID)
	}
}

func callLogIDValue(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func startFlickrCallLog(ctx context.Context, pool *pgxpool.Pool) (flickrclient.CallObserver, func()) {
	if pool == nil {
		return flickrCallLog{}, func() {}
	}
	recorder := quotaapp.NewCallRecorder(quotaPostgres.CallLog{Pool: pool}, 10_000, 500, time.Second, nil)
	recorderCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.Run(recorderCtx, func(err error) {
			slog.Default().WarnContext(context.Background(), "flickr call log batch failed", "error", err)
		})
	}()
	return flickrCallLog{recorder: recorder}, func() {
		cancel()
		<-done
	}
}
