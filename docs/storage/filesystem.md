---
tags:
  - storage
  - filesystem
  - kubernetes
---

# Local Filesystem

Filesystem storage writes artifacts to a shared volume mounted by both the
Version controller and Server. It is suitable for development, testing, and
air-gapped environments when paired with a PersistentVolumeClaim.

## Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `directoryPath` | string | Yes with `fileSystem` | Artifact directory; must match `storage.filesystem.mountPath` |

The Helm values are:

| Value | Default | Description |
|-------|---------|-------------|
| `storage.filesystem.enabled` | `false` | Enable the shared filesystem volume |
| `storage.filesystem.mountPath` | `/data/modules` | Container mount path and Server download guard root |
| `storage.filesystem.hostPath` | `""` | Host path for local development with kind |
| `storage.filesystem.storageClassName` | `""` | StorageClass for the PVC; must support `ReadWriteMany` |
| `storage.filesystem.size` | `10Gi` | PVC size |

!!! note
    Set the CRD `directoryPath` to the same value as
    `storage.filesystem.mountPath`. This pairing is required for filesystem
    downloads to pass the Server path guard.

## Local Development with kind

```bash
helm install opendepot opendepot/opendepot \
  -n opendepot-system \
  --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/tmp/opendepot-modules
```

With `hostPath`, the chart adds an init container that runs as root to set
ownership to UID `65532`, the non-root user used by the application containers.
This is intended for local development only and is not recommended for
production.

## Download Path Enforcement

The Server requires every filesystem download path to resolve inside the
configured mount directory. Requests outside this root return HTTP 403. The
chart passes `storage.filesystem.mountPath` to the Server automatically through
the `--filesystem-mount-path` flag.

## Production with a PVC

```bash
helm install opendepot opendepot/opendepot \
  -n opendepot-system \
  --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.storageClassName=efs-sc \
  --set storage.filesystem.size=50Gi
```

The PVC requires a StorageClass that supports `ReadWriteMany`, such as AWS EFS,
Azure Files, or NFS.

```yaml
storageConfig:
  fileSystem:
    directoryPath: /data/modules
```
