package planrisk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Question is a TypeSafe System One question (noul, score, or choice).
type Question struct {
	Type         string `json:"type"`
	Instructions any    `json:"instructions"`
	Criteria     any    `json:"criteria,omitempty"`
}

// Answer is a TypeSafe answer. Only the fields for its type are set.
type Answer struct {
	Type          string             `json:"type"`
	Noul          float64            `json:"noul"`
	Score         float64            `json:"score"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// Response is a TypeSafe System One response.
type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   struct {
		InputTokens int `json:"input_tokens"`
	} `json:"usage"`
}

// Evaluator answers questions about a state.
type Evaluator interface {
	Evaluate(ctx context.Context, state any, questions map[string]Question) (*Response, error)
}

// TypeSafeClient calls POST /v1/systemone. It retries rate limits and server
// errors with backoff, honoring Retry-After.
type TypeSafeClient struct {
	BaseURL    string
	APIKey     string
	Model      string
	HTTPClient *http.Client
	MaxRetries int
}

type request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Evaluate implements Evaluator.
func (c *TypeSafeClient) Evaluate(ctx context.Context, state any, questions map[string]Question) (*Response, error) {
	body, err := json.Marshal(request{State: state, Model: c.Model, Questions: questions})
	if err != nil {
		return nil, err
	}
	retries := c.MaxRetries
	if retries == 0 {
		retries = 3
	}
	backoff := 500 * time.Millisecond
	for attempt := 0; ; attempt++ {
		resp, retryAfter, err := c.do(ctx, body)
		if err == nil {
			return resp, nil
		}
		if retryAfter < 0 || attempt >= retries {
			return nil, err
		}
		wait := backoff << attempt
		if retryAfter > 0 {
			wait = retryAfter
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

// do performs one call. retryAfter is <0 for non-retryable errors, 0 for
// retryable errors without a server hint.
func (c *TypeSafeClient) do(ctx context.Context, body []byte) (*Response, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("calling TypeSafe: %w", err)
	}
	defer resp.Body.Close() // nolint: errcheck
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		var wait time.Duration
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			wait = time.Duration(s) * time.Second
		}
		return nil, wait, fmt.Errorf("TypeSafe returned %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, -1, fmt.Errorf("TypeSafe returned %s: %s", resp.Status, msg)
	}
	var out Response
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, -1, fmt.Errorf("decoding TypeSafe response: %w", err)
	}
	return &out, 0, nil
}
