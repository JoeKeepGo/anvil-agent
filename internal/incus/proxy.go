package incus

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type ProxyRequest struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

type ProxyResponse struct {
	ID     string          `json:"id"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
	Error  string          `json:"error,omitempty"`
}

func (c *Client) Execute(ctx context.Context, req *ProxyRequest) *ProxyResponse {
	var bodyReader io.Reader
	if req.Body != nil {
		bodyReader = io.NopCloser(&io.LimitedReader{
			R: bytesReader(req.Body),
			N: int64(len(req.Body)),
		})
	}

	resp, err := c.Do(ctx, req.Method, req.Path, bodyReader)
	if err != nil {
		return &ProxyResponse{
			ID:     req.ID,
			Status: http.StatusServiceUnavailable,
			Error:  err.Error(),
		}
	}

	respBody, err := ReadBody(resp)
	if err != nil {
		return &ProxyResponse{
			ID:     req.ID,
			Status: http.StatusInternalServerError,
			Error:  err.Error(),
		}
	}

	return &ProxyResponse{
		ID:     req.ID,
		Status: resp.StatusCode,
		Body:   json.RawMessage(respBody),
	}
}

type bytesReader []byte

func (b bytesReader) Read(p []byte) (int, error) {
	n := copy(p, b)
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}
