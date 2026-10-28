package roles

import (
	"context"
	"testing"
	"time"

	quotaapp "github.com/jooservices/xflickr/internal/quota/app"
	flickrclient "github.com/jooservices/xflickr/pkg/flickr"
)

type callLogWriterFunc func(context.Context, []quotaapp.CallRecord) error

func (f callLogWriterFunc) InsertCalls(ctx context.Context, records []quotaapp.CallRecord) error {
	return f(ctx, records)
}

func TestFlickrCallLogRecordsSafeMetadata(t *testing.T) {
	written := make(chan quotaapp.CallRecord, 1)
	recorder := quotaapp.NewCallRecorder(callLogWriterFunc(func(_ context.Context, records []quotaapp.CallRecord) error {
		written <- records[0]
		return nil
	}), 4, 1, time.Hour, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.Run(ctx, nil)
	}()
	startedAt := time.Now().UTC()
	userID, accountID, errorCode := int64(17), int64(29), int64(105)
	observer := flickrCallLog{recorder: recorder}
	observer.RecordCall(ctx, flickrclient.CallRecord{
		CallContext: flickrclient.CallContext{
			KeyID: 4, UserID: &userID, AccountID: &accountID, FlickrAccount: "123@example",
			Lane: "photo", Facet: "photo_sizes", Attempt: 2, NodeID: "worker-a", RequestID: flickrclient.NewRequestID(),
		},
		Method: "flickr.photos.getSizes", StartedAt: startedAt, Latency: 75 * time.Millisecond,
		HTTPStatus: 502, FlickrStat: "fail", FlickrErrorCode: 105, ErrorMessage: "upstream unavailable",
		ResponseBytes: 42, Outcome: "failure",
	})
	var got quotaapp.CallRecord
	select {
	case got = <-written:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("call record was not flushed")
	}
	cancel()
	<-done
	if got.KeyID != 4 || got.UserID == nil || *got.UserID != userID || got.AccountID == nil || *got.AccountID != accountID ||
		got.FlickrAccount != "123@example" || got.NodeID != "worker-a" || got.Method != "flickr.photos.getSizes" || got.Outcome != "failure" ||
		got.HTTPStatus == nil || *got.HTTPStatus != 502 || got.FlickrErrorCode == nil || *got.FlickrErrorCode != errorCode ||
		got.ErrorMessage != "upstream unavailable" || got.ResponseBytes != 42 || got.Lane != "photo" || got.Facet != "photo_sizes" || got.Attempt != 2 ||
		got.RequestID == "" || got.LatencyMS != 75 || !got.StartedAt.Equal(startedAt) {
		t.Fatalf("persisted call = %+v", got)
	}
}

func TestFlickrCallLogWithoutDatabaseAndCallLogIDs(t *testing.T) {
	_, stop := startFlickrCallLog(context.Background(), nil)
	stop()
	id := int64(7)
	if callLogIDValue(nil) != nil || callLogIDValue(&id) != id || callLogKeyIDValue(0) != nil || callLogKeyIDValue(id) != id {
		t.Fatal("callLogIDValue did not preserve nullable IDs")
	}
}

func TestFlickrCallLogRecordsKeyValidationBeforeKeyExists(t *testing.T) {
	written := make(chan quotaapp.CallRecord, 1)
	recorder := quotaapp.NewCallRecorder(callLogWriterFunc(func(_ context.Context, records []quotaapp.CallRecord) error {
		written <- records[0]
		return nil
	}), 4, 1, time.Hour, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		recorder.Run(ctx, nil)
	}()
	userID := int64(17)
	requestID := flickrclient.NewRequestID()
	observer := flickrCallLog{recorder: recorder}
	observer.RecordCall(ctx, flickrclient.CallRecord{
		CallContext: flickrclient.CallContext{UserID: &userID, Lane: "interactive", Facet: "key_validation", Attempt: 1, RequestID: requestID},
		Method:      "flickr.test.echo", StartedAt: time.Now().UTC(), HTTPStatus: 200, FlickrStat: "ok", Outcome: "success",
	})
	select {
	case got := <-written:
		if got.KeyID != 0 || got.UserID == nil || *got.UserID != userID || got.Method != "flickr.test.echo" ||
			got.Lane != "interactive" || got.Facet != "key_validation" || got.Attempt != 1 || got.RequestID != requestID {
			cancel()
			<-done
			t.Fatalf("persisted pre-key probe = %+v", got)
		}
	case <-time.After(time.Second):
		cancel()
		<-done
		t.Fatal("pre-key probe call was not recorded")
	}
	cancel()
	<-done
}
