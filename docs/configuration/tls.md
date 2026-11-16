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

The server still listens on port `8080` with TLS enabled. The chart switches the probes to HTTPS. The Service `targetPort` stays `8080`.

When `server.tls.enabled` and `ui.enabled` are `true`, the UI's NGINX proxy verifies the server certificate and hostname on every upstream request (`proxy_ssl_verify on`). It trusts the UI image's system CA bundle by default. For a private CA, add its PEM certificate as the `ca.crt` key in the `opendepot-tls` Secret and set `ui.serverCACertPath: /etc/tls/ca.crt`. The expected certificate name defaults to `server.<global.namespace>.svc.cluster.local`. Set `ui.serverTLSName` to override it.

!!! note
    With `server.anonymousAuth` enabled, the server uses its own ServiceAccount for Kubernetes API calls. Clients need no credentials. The server's ClusterRole grants read-only access to the OpenDepot resources it serves. With anonymous auth enabled, the policy API rejects non-`GET` requests. Write access to `scanpolicies` exists only when `server.policyManagement.enabled` is `true`.

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
