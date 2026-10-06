---
tags:
  - configuration
  - tls
  - security
---

# TLS Configuration

## Direct TLS on the Server

Set `server.tls.enabled: true` in your Helm values and provide a TLS Secret named `opendepot-tls`:

```yaml
server:
  tls:
    enabled: true
    certPath: /etc/tls/tls.crt
    keyPath: /etc/tls/tls.key
```
!!! note
    When TLS is enabled, the server listens on port `443` instead of `8080`. Ensure your Service `targetPort` and any probes are updated accordingly.

When `server.tls.enabled=true` and the UI is enabled, its NGINX proxy verifies the server certificate and hostname for all upstream HTTPS requests by default. It trusts the UI image's system CA bundle unless `ui.serverCACertPath` points to a private CA certificate. To use a private CA, include its PEM certificate as the `ca.crt` key in the `opendepot-tls` Secret and set `ui.serverCACertPath: /etc/tls/ca.crt`. The expected certificate DNS name defaults to `server.<global.namespace>.svc.cluster.local`; set `ui.serverTLSName` when using a custom server certificate name.

!!! note
    When `anonymousAuth` is enabled, the server uses its own ServiceAccount to query the Kubernetes API for Module and Version resources. No client credentials are required. The server's ClusterRole only permits reading `modules` and `versions`, so anonymous users cannot create or modify resources.

## TLS via Istio Ingress Gateway

For TLS termination at the Istio ingress gateway, enable the Istio VirtualService and create a Gateway resource. The chart's VirtualService references the gateway `istio-ingress/istio-ingress-gateway` by default. Store your TLS certificate as a Secret in the `istio-ingress` namespace:

```yaml
server:
  ingress:
    istio:
      enabled: true
      hosts:
        - opendepot.defdev.io
```
