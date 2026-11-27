package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteOCIErrorEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOCIError(rec, http.StatusNotFound, ociCodeManifestUnknown, "manifest unknown to registry")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}

	if got := rec.Header().Get(ociTestContentType); got != ociContentTypeJSON {
		t.Fatalf("%s = %q, want %s", ociTestContentType, got, ociContentTypeJSON)
	}

	if got := rec.Header().Get(ociAPIVersionHeader); got != ociAPIVersion {
		t.Fatalf("%s = %q, want %s", ociAPIVersionHeader, got, ociAPIVersion)
	}

	want := `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown to registry"}]}` + "\n"
	if rec.Body.String() != want {
		t.Fatalf("body = %q, want %q", rec.Body.String(), want)
	}
}

func TestWriteOCIAuthFailure(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		wantCode  int
		wantError string
		challenge bool
	}{
		{
			name:      "unauthorized carries the Basic challenge",
			status:    http.StatusUnauthorized,
			wantCode:  http.StatusUnauthorized,
			wantError: ociCodeUnauthorized,
			challenge: true,
		},
		{name: "forbidden is DENIED", status: http.StatusForbidden, wantCode: http.StatusForbidden, wantError: ociCodeDenied},
		{
			name:      "other failures are UNKNOWN",
			status:    http.StatusConflict,
			wantCode:  http.StatusInternalServerError,
			wantError: ociCodeUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeOCIAuthFailure(rec, tc.status)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}

			if got := ociTestErrorCode(t, rec); got != tc.wantError {
				t.Fatalf("code = %q, want %q", got, tc.wantError)
			}

			hasChallenge := rec.Header().Get("WWW-Authenticate") == ociTestChallenge
			if hasChallenge != tc.challenge {
				t.Fatalf("WWW-Authenticate challenge present = %t, want %t", hasChallenge, tc.challenge)
			}
		})
	}
}
