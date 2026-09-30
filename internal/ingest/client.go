// Package ingest provides a small client for posting gzip-compressed JSON
// payloads to the Farsight ingest API, shared by the agent's snapshot POST
// and (in later plans) other ingest kinds such as events.
package ingest

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client posts JSON payloads, gzip-compressed, to a Farsight ingest
// endpoint for one server.
type Client struct {
	baseURL  string
	serverID string
	token    string
	client   *http.Client
}

// New returns a Client for the given base URL, server id, and bearer
// token. Any trailing "/" on baseURL is trimmed.
func New(baseURL, serverID, token string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		serverID: serverID,
		token:    token,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

// StatusError is a non-2xx response from the ingest API. Callers use Code
// to tell a batch the server will never accept (e.g. 400) from a transient
// failure worth retrying.
type StatusError struct {
	Code int
	Body string // the first 512 bytes of the response body, trimmed
}

func (e *StatusError) Error() string { return fmt.Sprintf("status %d: %s", e.Code, e.Body) }

// Post gzip-compresses the JSON encoding of v and POSTs it to
// {baseURL}/ingest/{serverID}/{kind}. A non-2xx response is returned as a
// *StatusError carrying the status code and the first 512 bytes of the
// response body.
func (c *Client) Post(ctx context.Context, kind string, v any) error {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/ingest/%s/%s", c.baseURL, c.serverID, kind)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &StatusError{Code: resp.StatusCode, Body: string(bytes.TrimSpace(body))}
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}
