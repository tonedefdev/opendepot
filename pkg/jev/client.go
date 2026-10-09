package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultEndpoint is the TypeSafe Jev API endpoint used when no endpoint option is supplied.
	DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"

	// Model is the model alias sent with every request.
	Model = "jev-latest"

	// MaxStateBytes is the largest state accepted for assessment.
	MaxStateBytes = 64 << 10

	maxResponseBytes = 1 << 20
	maxErrorBytes    = 256
	maxRetries       = 3
	defaultTimeout   = 30 * time.Second
	defaultBackoff   = 500 * time.Millisecond
)

// Option configures a Client.
type Option func(*Client)

// WithEndpoint overrides the default Jev API endpoint. An empty value keeps the default.
func WithEndpoint(endpoint string) Option {
	return func(c *Client) {
		if endpoint != "" {
			c.endpoint = endpoint
		}
	}
}

// Client calls the Jev API.
type Client struct {
	endpoint   string
	httpClient *http.Client
	baseDelay  time.Duration
}

// APIError is returned when the Jev API responds with a non-2xx status. The message is redacted.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("jev: API returned status %d: %s", e.StatusCode, e.Message)
}

// Result is the typed assessment of a state.
type Result struct {
	Model                         string
	SafeProbability               float64
	PromptInjectionProbability    float64
	DataExfiltrationProbability   float64
	DestructiveActionsProbability float64
	HiddenInstructionsProbability float64
	ScopeMismatchProbability      float64
	RemoteExecutionProbability    float64
	RiskScore                     float64
	RiskLevel                     string
	RiskConfidence                float64
}

// NewClient returns a Client that uses DefaultEndpoint unless overridden by opts.
func NewClient(opts ...Option) *Client {
	c := &Client{
		endpoint: DefaultEndpoint,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return errors.New("jev: stopped after 10 redirects")
				}

				return validateEndpoint(req.URL.String())
			},
		},
		baseDelay: defaultBackoff,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

type request struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Assess evaluates state with the default question set for kind ("skill" or "agent") in a single request.
func (c *Client) Assess(ctx context.Context, token, kind, state string) (*Result, error) {
	if token == "" {
		return nil, errors.New("jev: token is required")
	}

	if kind != "skill" && kind != "agent" {
		return nil, fmt.Errorf("jev: unsupported kind %q", kind)
	}

	if len(state) > MaxStateBytes {
		return nil, fmt.Errorf("jev: state is %d bytes, exceeds the %d byte limit", len(state), MaxStateBytes)
	}

	body, err := json.Marshal(request{
		State:     state,
		Model:     Model,
		Questions: DefaultQuestions(kind),
	})
	if err != nil {
		return nil, fmt.Errorf("jev: marshal request: %w", err)
	}

	raw, err := c.do(ctx, token, body)
	if err != nil {
		return nil, err
	}

	return parseResponse(raw)
}

func (c *Client) do(ctx context.Context, token string, body []byte) ([]byte, error) {
	for attempt := 0; ; attempt++ {
		status, respBody, err := c.post(ctx, token, body)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}

			return nil, fmt.Errorf("jev: request failed: %s", redact(err.Error(), token))
		}

		if status >= 200 && status < 300 {
			return respBody, nil
		}

		apiErr := &APIError{
			StatusCode: status,
			Message:    redact(truncate(respBody, maxErrorBytes), token),
		}

		if !retryable(status) || attempt >= maxRetries {
			return nil, apiErr
		}

		timer := time.NewTimer(c.baseDelay * time.Duration(1<<attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// validateEndpoint requires https so the bearer token is never sent in cleartext. Plain http is
// accepted only for loopback hosts, which keeps local test servers working.
func validateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("jev: invalid endpoint: %w", err)
	}

	if u.Scheme == "https" {
		return nil
	}

	if u.Scheme == "http" && isLoopbackHost(u.Hostname()) {
		return nil
	}

	return fmt.Errorf("jev: endpoint must use https (http is allowed only for loopback hosts)")
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}

func (c *Client) post(ctx context.Context, token string, body []byte) (int, []byte, error) {
	if err := validateEndpoint(c.endpoint); err != nil {
		return 0, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("read response: %w", err)
	}

	return resp.StatusCode, respBody, nil
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func redact(msg, token string) string {
	if token == "" {
		return msg
	}

	return strings.ReplaceAll(msg, token, "[REDACTED]")
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}

	return string(b)
}

type response struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
}

func parseResponse(raw []byte) (*Result, error) {
	var resp response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("jev: decode response: %w", err)
	}

	if resp.Model == "" {
		return nil, errors.New("jev: response missing model")
	}

	result := &Result{Model: resp.Model}
	noul := []struct {
		id  string
		dst *float64
	}{
		{idSafe, &result.SafeProbability},
		{idPromptInjection, &result.PromptInjectionProbability},
		{idDataExfiltration, &result.DataExfiltrationProbability},
		{idDestructiveActions, &result.DestructiveActionsProbability},
		{idHiddenInstructions, &result.HiddenInstructionsProbability},
		{idScopeMismatch, &result.ScopeMismatchProbability},
		{idRemoteExecution, &result.RemoteExecutionProbability},
	}

	for _, q := range noul {
		v, err := parseNoul(resp.Answers, q.id)
		if err != nil {
			return nil, err
		}

		*q.dst = v
	}

	if err := parseRisk(resp.Answers, result); err != nil {
		return nil, err
	}

	return result, nil
}

func parseNoul(answers map[string]json.RawMessage, id string) (float64, error) {
	raw, ok := answers[id]
	if !ok {
		return 0, fmt.Errorf("jev: response missing answer %q", id)
	}

	var a struct {
		Type string   `json:"type"`
		Noul *float64 `json:"noul"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return 0, fmt.Errorf("jev: answer %q: %w", id, err)
	}

	if a.Type != "noul" || a.Noul == nil {
		return 0, fmt.Errorf("jev: answer %q is not a valid noul answer", id)
	}

	if *a.Noul < 0 || *a.Noul > 1 {
		return 0, fmt.Errorf("jev: answer %q: noul %g is outside [0, 1]", id, *a.Noul)
	}

	return *a.Noul, nil
}

func parseRisk(answers map[string]json.RawMessage, result *Result) error {
	raw, ok := answers[idRisk]
	if !ok {
		return fmt.Errorf("jev: response missing answer %q", idRisk)
	}

	var a struct {
		Type          string             `json:"type"`
		Score         *float64           `json:"score"`
		Legend        map[string]string  `json:"legend"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    *float64           `json:"confidence"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return fmt.Errorf("jev: answer %q: %w", idRisk, err)
	}

	if a.Type != "score" || a.Score == nil || a.Confidence == nil {
		return fmt.Errorf("jev: answer %q is not a valid score answer", idRisk)
	}

	if len(a.Legend) == 0 {
		return fmt.Errorf("jev: answer %q: legend is empty", idRisk)
	}

	if *a.Score < 0 || *a.Score > float64(len(a.Legend)-1) {
		return fmt.Errorf("jev: answer %q: score %g is outside the legend range", idRisk, *a.Score)
	}

	if *a.Confidence < 0 || *a.Confidence > 1 {
		return fmt.Errorf("jev: answer %q: confidence %g is outside [0, 1]", idRisk, *a.Confidence)
	}

	level, err := topLevel(a.Legend, a.Probabilities)
	if err != nil {
		return fmt.Errorf("jev: answer %q: %w", idRisk, err)
	}

	result.RiskScore = *a.Score
	result.RiskLevel = level
	result.RiskConfidence = *a.Confidence

	return nil
}

// topLevel returns the legend name with the highest probability. Ties resolve to the lowest index.
func topLevel(legend map[string]string, probabilities map[string]float64) (string, error) {
	var (
		best     string
		bestProb = -1.0
	)

	for i := range len(legend) {
		key := strconv.Itoa(i)

		name, ok := legend[key]
		if !ok {
			return "", fmt.Errorf("legend missing index %s", key)
		}

		p, ok := probabilities[key]
		if !ok {
			return "", fmt.Errorf("probabilities missing index %s", key)
		}

		if p < 0 || p > 1 {
			return "", fmt.Errorf("probability %g at index %s is outside [0, 1]", p, key)
		}

		if p > bestProb {
			best = name
			bestProb = p
		}
	}

	return best, nil
}
