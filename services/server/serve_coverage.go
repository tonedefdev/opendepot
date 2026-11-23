//go:build coverage

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// listenAndServe shuts down on SIGTERM and returns from main so GOCOVERDIR receives counters.
func listenAndServe(addr string, handler http.Handler) error {
	return serveUntilSignal(&http.Server{Addr: addr, Handler: handler}, "", "")
}

func listenAndServeTLS(addr, certFile, keyFile string, handler http.Handler) error {
	if certFile == "" || keyFile == "" {
		return errors.New("TLS requires both a certificate file and a key file; refusing to serve plaintext")
	}

	return serveUntilSignal(&http.Server{Addr: addr, Handler: handler}, certFile, keyFile)
}

func serveUntilSignal(srv *http.Server, certFile, keyFile string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if certFile != "" {
			errCh <- srv.ListenAndServeTLS(certFile, keyFile)
			return
		}

		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}

	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}
