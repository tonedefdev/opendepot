package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	downloadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "opendepot_downloads_total",
			Help: "Total number of OpenDepot module and provider downloads recorded by this server process.",
		},
		[]string{"namespace", "kind", "name", "version", "server_instance"},
	)
	downloadLastTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "opendepot_download_last_timestamp_seconds",
			Help: "Unix timestamp of the most recent OpenDepot download recorded by this server process.",
		},
		[]string{"namespace", "kind", "name", "version", "server_instance"},
	)

	metricsRegistry = prometheus.NewRegistry()
	serverInstance  = newServerInstance()
)

func init() {
	metricsRegistry.MustRegister(downloadsTotal, downloadLastTimestamp)
	metricsRegistry.MustRegister(newRegistryMetricsCollector())
}

func newServerInstance() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("unknown-%d", time.Now().UnixNano())
	}

	return hex.EncodeToString(value)
}

func recordDownload(_ context.Context, namespace, kind, name, version string) error {
	downloadsTotal.WithLabelValues(namespace, kind, name, version, serverInstance).Inc()
	downloadLastTimestamp.WithLabelValues(namespace, kind, name, version, serverInstance).Set(float64(time.Now().Unix()))

	return nil
}

type metricsServer struct {
	server *http.Server
}

func newMetricsServer(address string) *metricsServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{}))

	return &metricsServer{server: &http.Server{Addr: address, Handler: mux}}
}

func (s *metricsServer) Start() error {
	err := s.server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}

	return err
}
