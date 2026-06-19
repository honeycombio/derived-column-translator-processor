// Package honeycomb is a minimal client for the Honeycomb Configuration API,
// scoped to the operations this project needs: listing the derived columns
// defined on a dataset or environment.
package honeycomb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultAPIURL is the Honeycomb US API endpoint. EU users override this.
	DefaultAPIURL = "https://api.honeycomb.io"

	// AllDatasets is the special dataset token for environment-wide derived columns.
	AllDatasets = "__all__"

	headerTeam      = "X-Honeycomb-Team"
	headerUserAgent = "User-Agent"
	userAgent       = "honeycomb-derived-column-translator"
)

// DerivedColumn mirrors the JSON returned by the Honeycomb derived_columns API.
// Field tags match hound/api/public/external.DerivedColumn.
type DerivedColumn struct {
	ID          string `json:"id"`
	Alias       string `json:"alias"`
	Expression  string `json:"expression"`
	Description string `json:"description"`
}

// Client talks to the Honeycomb Configuration API.
type Client struct {
	apiKey     string
	apiURL     string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithAPIURL overrides the API endpoint (e.g. the EU instance).
func WithAPIURL(url string) Option {
	return func(c *Client) {
		if url != "" {
			c.apiURL = url
		}
	}
}

// WithHTTPClient supplies a custom http.Client (timeouts, proxy, TLS).
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// NewClient builds a Client. The API key is required; everything else has a default.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:     apiKey,
		apiURL:     DefaultAPIURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ListDerivedColumns returns every derived column defined on the given dataset.
// Pass an empty string (or AllDatasets) to list environment-wide derived columns.
func (c *Client) ListDerivedColumns(ctx context.Context, dataset string) ([]DerivedColumn, error) {
	if dataset == "" {
		dataset = AllDatasets
	}

	url := fmt.Sprintf("%s/1/derived_columns/%s", strings.TrimRight(c.apiURL, "/"), dataset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set(headerTeam, c.apiKey)
	req.Header.Set(headerUserAgent, userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing derived columns for %q: %w", dataset, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("listing derived columns for %q failed: %s: %s",
			dataset, resp.Status, strings.TrimSpace(string(body)))
	}

	var cols []DerivedColumn
	if err := json.NewDecoder(resp.Body).Decode(&cols); err != nil {
		return nil, fmt.Errorf("decoding derived columns response: %w", err)
	}
	return cols, nil
}
