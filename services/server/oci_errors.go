package main

import (
	"encoding/json"
	"net/http"
)

// OCI Distribution error codes returned by the /v2/ read API. See the OCI Distribution specification, "Error Codes".
const (
	ociCodeBlobUnknown     = "BLOB_UNKNOWN"
	ociCodeDenied          = "DENIED"
	ociCodeDigestInvalid   = "DIGEST_INVALID"
	ociCodeManifestUnknown = "MANIFEST_UNKNOWN"
	ociCodeNameInvalid     = "NAME_INVALID"
	ociCodeNameUnknown     = "NAME_UNKNOWN"
	ociCodeTagInvalid      = "TAG_INVALID"
	ociCodeUnauthorized    = "UNAUTHORIZED"
	ociCodeUnknown         = "UNKNOWN"
	ociCodeUnsupported     = "UNSUPPORTED"
)

const (
	ociAPIVersionHeader = "Docker-Distribution-Api-Version"
	ociAPIVersion       = "registry/2.0"
	ociContentTypeJSON  = "application/json"
)

// ociErrorItem is one entry of the OCI error envelope.
type ociErrorItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ociErrorEnvelope is the body of every OCI Distribution error response.
type ociErrorEnvelope struct {
	Errors []ociErrorItem `json:"errors"`
}

// ociRouteError is a request error found while parsing the route, before any cluster or storage read.
type ociRouteError struct {
	status  int
	code    string
	message string
}

// writeOCIError writes the OCI error envelope with the given HTTP status.
func writeOCIError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", ociContentTypeJSON)
	w.Header().Set(ociAPIVersionHeader, ociAPIVersion)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ociErrorEnvelope{Errors: []ociErrorItem{{Code: code, Message: message}}})
}

// writeOCIAuthFailure writes the OCI response for a failed authentication. A 401 carries the Basic challenge.
func writeOCIAuthFailure(w http.ResponseWriter, status int) {
	switch status {
	case http.StatusUnauthorized:
		setOCIChallenge(w)
		writeOCIError(w, status, ociCodeUnauthorized, "authentication required")
	case http.StatusForbidden:
		writeOCIError(w, status, ociCodeDenied, "access to the requested resource is not authorized")
	default:
		writeOCIError(w, http.StatusInternalServerError, ociCodeUnknown, "internal server error")
	}
}
