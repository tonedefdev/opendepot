//go:build !coverage

package main

import "net/http"

func listenAndServe(addr string, handler http.Handler) error {
	return http.ListenAndServe(addr, handler)
}

func listenAndServeTLS(addr, certFile, keyFile string, handler http.Handler) error {
	return http.ListenAndServeTLS(addr, certFile, keyFile, handler)
}
