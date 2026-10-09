package jev

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sentinelToken = "sk-sentinel-SECRET-token-12345"

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()

	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return b
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := NewClient(WithEndpoint(srv.URL))
	c.baseDelay = time.Millisecond

	return c, srv
}

func TestAssessParsesResponse(t *testing.T) {
	fixture := loadFixture(t, "response_ok.json")

	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer "+sentinelToken {
			t.Errorf("Authorization = %q, want bearer token", got)
		}

		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}

		var body struct {
			State     string              `json:"state"`
			Model     string              `json:"model"`
			Questions map[string]Question `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}

		if body.Model != "jev-latest" {
			t.Errorf("model = %q, want jev-latest", body.Model)
		}

		if body.State != "state content" {
			t.Errorf("state = %q, want state content", body.State)
		}

		noul, score := 0, 0
		for _, q := range body.Questions {
			switch q.Type {
			case "noul":
				noul++
			case "score":
				score++
			}
		}

		if len(body.Questions) != 8 || noul != 7 || score != 1 {
			t.Errorf("questions: got %d total, %d noul, %d score", len(body.Questions), noul, score)
		}

		_, _ = w.Write(fixture)
	})

	got, err := c.Assess(context.Background(), sentinelToken, "skill", "state content")
	if err != nil {
		t.Fatalf("Assess() error = %v", err)
	}

	want := &Result{
		Model:                         "jev-1.13.0",
		SafeProbability:               0.99,
		PromptInjectionProbability:    0.02,
		DataExfiltrationProbability:   0.01,
		DestructiveActionsProbability: 0.03,
		HiddenInstructionsProbability: 0.04,
		ScopeMismatchProbability:      0.05,
		RemoteExecutionProbability:    0.06,
		RiskScore:                     0.75,
		RiskLevel:                     "Minimal",
		RiskConfidence:                0.88,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Assess() = %+v, want %+v", got, want)
	}
}

func TestAssessRetriesServerError(t *testing.T) {
	fixture := loadFixture(t, "response_ok.json")

	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		_, _ = w.Write(fixture)
	})

	if _, err := c.Assess(context.Background(), sentinelToken, "agent", "state"); err != nil {
		t.Fatalf("Assess() error = %v", err)
	}

	if n := calls.Load(); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
}

func TestAssessRetriesRateLimitThenGivesUp(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.Assess(context.Background(), sentinelToken, "skill", "state")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("Assess() error = %v, want APIError 429", err)
	}

	if n := calls.Load(); n != maxRetries+1 {
		t.Errorf("calls = %d, want %d", n, maxRetries+1)
	}
}

func TestAssessDoesNotRetryClientError(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad request")
	})

	_, err := c.Assess(context.Background(), sentinelToken, "skill", "state")

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("Assess() error = %v, want APIError 400", err)
	}

	if n := calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestAssessRedactsToken(t *testing.T) {
	t.Run("echoed in API error body", func(t *testing.T) {
		c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, "invalid credentials: "+r.Header.Get("Authorization"))
		})

		_, err := c.Assess(context.Background(), sentinelToken, "skill", "state")
		if err == nil {
			t.Fatal("Assess() error = nil, want error")
		}

		assertNoToken(t, err)
	})

	t.Run("transport failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		endpoint := srv.URL
		srv.Close()

		c := NewClient(WithEndpoint(endpoint))

		_, err := c.Assess(context.Background(), sentinelToken, "skill", "state")
		if err == nil {
			t.Fatal("Assess() error = nil, want error")
		}

		assertNoToken(t, err)
	})
}

func assertNoToken(t *testing.T, err error) {
	t.Helper()

	if strings.Contains(err.Error(), sentinelToken) {
		t.Errorf("error leaks token: %v", err)
	}
}

func TestAssessMalformedResponse(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		fixture string
	}{
		{name: "missing answer", fixture: "response_missing_answer.json"},
		{name: "wrong answer type", fixture: "response_wrong_type.json"},
		{name: "not json", body: "<html>oops</html>"},
		{name: "missing model", body: `{"answers":{}}`},
		{name: "noul out of range", body: strings.Replace(string(loadFixture(t, "response_ok.json")), `"noul": 0.99`, `"noul": 1.5`, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := []byte(tt.body)
			if tt.fixture != "" {
				payload = loadFixture(t, tt.fixture)
			}

			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(payload)
			})

			got, err := c.Assess(context.Background(), sentinelToken, "skill", "state")
			if err == nil {
				t.Fatalf("Assess() = %+v, want error", got)
			}

			if got != nil {
				t.Errorf("Assess() result = %+v with error, want nil", got)
			}
		})
	}
}

func TestAssessRejectsOversizedState(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	})

	state := strings.Repeat("a", MaxStateBytes+1)
	if _, err := c.Assess(context.Background(), sentinelToken, "skill", state); err == nil {
		t.Fatal("Assess() error = nil, want error")
	}

	if n := calls.Load(); n != 0 {
		t.Errorf("calls = %d, want 0", n)
	}
}

func TestAssessRequiresTokenAndKind(t *testing.T) {
	c := NewClient()

	if _, err := c.Assess(context.Background(), "", "skill", "state"); err == nil {
		t.Error("Assess() with empty token: error = nil, want error")
	}

	if _, err := c.Assess(context.Background(), sentinelToken, "module", "state"); err == nil {
		t.Error("Assess() with unsupported kind: error = nil, want error")
	}
}

func TestAssessHonorsContextCancellation(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c.baseDelay = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.Assess(ctx, sentinelToken, "skill", "state")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Assess() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestDefaultQuestions(t *testing.T) {
	q := DefaultQuestions("agent")

	wantIDs := []string{
		"safe", "prompt_injection", "data_exfiltration", "destructive_actions",
		"hidden_instructions", "scope_mismatch", "remote_execution", "risk",
	}
	if len(q) != len(wantIDs) {
		t.Fatalf("len(questions) = %d, want %d", len(q), len(wantIDs))
	}

	for _, id := range wantIDs {
		question, ok := q[id]
		if !ok {
			t.Errorf("missing question %q", id)
			continue
		}

		if !strings.Contains(question.Instructions, "agent") {
			t.Errorf("question %q does not use kind: %q", id, question.Instructions)
		}
	}

	if q["risk"].Type != "score" {
		t.Errorf("risk type = %q, want score", q["risk"].Type)
	}

	wantCriteria := []string{"Minimal", "Low", "Moderate", "High", "Critical"}
	if !reflect.DeepEqual(q["risk"].Criteria, wantCriteria) {
		t.Errorf("risk criteria = %v, want %v", q["risk"].Criteria, wantCriteria)
	}
}

func TestWithEndpointDefault(t *testing.T) {
	if c := NewClient(); c.endpoint != DefaultEndpoint {
		t.Errorf("endpoint = %q, want %q", c.endpoint, DefaultEndpoint)
	}

	if c := NewClient(WithEndpoint("")); c.endpoint != DefaultEndpoint {
		t.Errorf("endpoint with empty override = %q, want %q", c.endpoint, DefaultEndpoint)
	}

	if c := NewClient(); c.httpClient.Timeout != 30*time.Second {
		t.Errorf("timeout = %v, want 30s", c.httpClient.Timeout)
	}
}

func TestValidateEndpoint(t *testing.T) {
	cases := map[string]bool{
		"https://api.typesafe.ai/v1/systemone": true,
		"http://127.0.0.1:8080/v1":             true,
		"http://localhost:8080/v1":             true,
		"http://[::1]:8080/v1":                 true,
		"http://api.typesafe.ai/v1/systemone":  false,
		"HTTP://evil.example/x":                false,
		"ftp://api.typesafe.ai/v1":             false,
		"not a url :// bad":                    false,
	}
	for endpoint, wantOK := range cases {
		err := validateEndpoint(endpoint)
		if (err == nil) != wantOK {
			t.Fatalf("validateEndpoint(%q) error = %v, want ok=%v", endpoint, err, wantOK)
		}
	}
}

func TestCheckRedirectRevalidatesEndpoint(t *testing.T) {
	c := NewClient()
	check := c.httpClient.CheckRedirect

	newRequest := func(t *testing.T, rawURL string) *http.Request {
		t.Helper()

		req, err := http.NewRequest(http.MethodPost, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}

		return req
	}

	via := []*http.Request{newRequest(t, "https://api.typesafe.ai/v1/systemone")}
	cases := map[string]bool{
		"https://api.typesafe.ai/v2/systemone": true,
		"http://127.0.0.1:8080/v1":             true,
		"http://api.typesafe.ai/v1/systemone":  false,
		"http://evil.example/steal":            false,
	}
	for target, wantOK := range cases {
		err := check(newRequest(t, target), via)
		if (err == nil) != wantOK {
			t.Fatalf("CheckRedirect(%q) error = %v, want ok=%v", target, err, wantOK)
		}
	}

	tenRedirects := make([]*http.Request, 10)
	for i := range tenRedirects {
		tenRedirects[i] = newRequest(t, "https://api.typesafe.ai/v1/systemone")
	}

	if err := check(newRequest(t, "https://api.typesafe.ai/v1/systemone"), tenRedirects); err == nil {
		t.Fatal("CheckRedirect() allowed an eleventh redirect, want error")
	}
}
