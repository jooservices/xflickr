package roles

import (
	"fmt"
	"io"
	"net/http"
)

type streamResponse struct{ Body io.ReadCloser }

func (response streamResponse) VisitStreamEventsResponse(w http.ResponseWriter) error {
	if response.Body == nil {
		return io.ErrUnexpectedEOF
	}
	defer response.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, canFlush := w.(http.Flusher)
	buffer := make([]byte, 4096)
	for {
		n, err := response.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				return writeErr
			}
			if canFlush {
				flusher.Flush()
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("SSE stream read: %w", err)
		}
	}
}
