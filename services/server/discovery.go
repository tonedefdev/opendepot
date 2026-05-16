package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

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

func normalizeVersion(versionString string) string {
	return strings.TrimPrefix(strings.TrimSpace(versionString), "v")
}

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
