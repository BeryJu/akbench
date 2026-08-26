// Package grafana posts region annotations for TestRuns, reproducing the
// Ansible harness's annotate.yml behaviour (a purple band on every dashboard
// covering a run, tagged so it can be found again).
package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Client posts annotations to a Grafana instance using a service-account
// token (never the Ansible harness's anonymous-admin approach).
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// FromEnv builds a Client from BENCH_GRAFANA_URL/BENCH_GRAFANA_TOKEN. Both
// empty is a valid, deliberate "annotations disabled" configuration - M1 has
// no Grafana deployed yet, per the repo plan.
func FromEnv() *Client {
	return &Client{
		BaseURL: os.Getenv("BENCH_GRAFANA_URL"),
		Token:   os.Getenv("BENCH_GRAFANA_TOKEN"),
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled reports whether this client has enough configuration to post
// annotations.
func (c *Client) Enabled() bool {
	return c != nil && c.BaseURL != ""
}

type annotationRequest struct {
	Time    int64    `json:"time"`
	TimeEnd int64    `json:"timeEnd,omitempty"`
	Tags    []string `json:"tags"`
	Text    string   `json:"text"`
}

type annotationResponse struct {
	ID int64 `json:"id"`
}

// Annotate posts a region (or point, if end is zero) annotation and returns
// its ID for storage in TestRun.status.grafanaAnnotationID.
func (c *Client) Annotate(ctx context.Context, start, end time.Time, tags []string, text string) (int64, error) {
	if !c.Enabled() {
		return 0, nil
	}
	body := annotationRequest{
		Time: start.UnixMilli(),
		Tags: tags,
		Text: text,
	}
	if !end.IsZero() {
		body.TimeEnd = end.UnixMilli()
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/annotations", bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("grafana annotation POST returned %s", resp.Status)
	}
	var out annotationResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.ID, nil
}
