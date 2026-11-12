package s3

import (
	"bufio"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// readPayload returns the object bytes of a request body. minio-go signs a
// streaming payload as aws-chunked frames, which S3 strips before storing.
func readPayload(r *http.Request) ([]byte, error) {
	if !strings.HasPrefix(r.Header.Get("X-Amz-Content-Sha256"), "STREAMING-") {
		return io.ReadAll(r.Body)
	}
	return decodeAWSChunked(r.Body)
}

// decodeAWSChunked reads frames of the form "<hex size>;chunk-signature=<sig>\r\n<data>\r\n"
// until the zero-size frame.
func decodeAWSChunked(body io.Reader) ([]byte, error) {
	reader := bufio.NewReader(body)
	var data []byte
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		sizeText, _, _ := strings.Cut(strings.TrimSpace(line), ";")
		size, err := strconv.ParseInt(sizeText, 16, 64)
		if err != nil {
			return nil, err
		}
		if size == 0 {
			return data, nil
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(reader, chunk); err != nil {
			return nil, err
		}
		data = append(data, chunk...)
		if _, err := reader.ReadString('\n'); err != nil {
			return nil, err
		}
	}
}
