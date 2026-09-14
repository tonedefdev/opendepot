---
tags:
  - authentication
  - kubernetes
  - development
---

# Base64-Encoded Kubeconfig

Use a base64-encoded kubeconfig for local development or environments where an
environment variable is not practical. This method is not recommended for
production because the credential is long-lived and manually rotated.

!!! note
    This method requires `server.useBearerToken: false` in your Helm values.

## Encode Your Kubeconfig

```bash
kubectl config view --raw | base64 | tr -d '\n' > /tmp/kubeconfig.b64
```

## Create the Credentials File

Create `~/.terraform.d/credentials.tfrc.json` and replace the placeholder with
the contents of `/tmp/kubeconfig.b64`:

```json
{
  "credentials": {
    "opendepot.defdev.io": {
      "token": "<contents-of-kubeconfig.b64>"
    }
  }
}
```

Restrict the file permissions:

```bash
chmod 600 ~/.terraform.d/credentials.tfrc.json
```

OpenTofu and Terraform read this file automatically when they contact the
registry.
