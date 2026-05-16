package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// serviceDiscoveryHandler serves the Terraform service-discovery document at
// /.well-known/terraform.json. It advertises the module and provider registry
// base URLs and, when OIDC is configured, the login.v1 endpoints required by
// the OpenTofu CLI login flow.
func serviceDiscoveryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := ServiceDiscoveryResponse{
		ModulesURL:   "/opendepot/modules/v1/",
		ProvidersURL: "/opendepot/providers/v1/",
	}

	if oidcProvider != nil {
		endpoints := oidcProvider.Endpoint()
		response.LoginV1 = &LoginV1Info{
			Client:     *opendepotOIDCClientID,
			GrantTypes: []string{"authz_code", "device_code"},
			Authz:      endpoints.AuthURL,
			Token:      endpoints.TokenURL,
			Ports:      []int{10000, 10001, 10002, 10003, 10004, 10005, 10006, 10007, 10008, 10009, 10010},
		}
	}

	json.NewEncoder(w).Encode(response)
}

// normalizeVersion strips a leading "v" prefix and surrounding whitespace from a
// version string so that "v1.2.3" and "1.2.3" compare as equal.
func normalizeVersion(versionString string) string {
	return strings.TrimPrefix(strings.TrimSpace(versionString), "v")
}

// requestBaseURL derives the scheme and host of the incoming request. It honours
// TLS termination at the server, and falls back to the X-Forwarded-Proto header
// set by a reverse proxy, before defaulting to http.
func requestBaseURL(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil {
		if fwdProto := r.Header.Get("X-Forwarded-Proto"); fwdProto != "" {
			scheme = fwdProto
		} else {
			scheme = "http"
		}
	}

	return fmt.Sprintf("%s://%s", scheme, r.Host)
}
