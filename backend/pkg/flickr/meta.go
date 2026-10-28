package flickr

import "time"

// CallMeta contains transport details for one Flickr API call.
type CallMeta struct {
	HTTPStatus   int
	Latency      time.Duration
	Bytes        int64
	ErrorCode    int
	StartedAt    time.Time
	FlickrStat   string
	ErrorMessage string
}
