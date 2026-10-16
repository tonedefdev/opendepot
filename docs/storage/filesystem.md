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
    The CRD `directoryPath` must be the same as or inside `storage.filesystem.mountPath`.
    Module downloads outside that root are rejected.

## Local Development with kind

```bash
helm install opendepot opendepot/opendepot \
  -n opendepot-system \
  --create-namespace \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/tmp/opendepot-modules
```

With `hostPath`, the chart adds an init container that runs as root to set
ownership of the mount path to UID `65532`, the non-root user used by the
application containers. Use this for local development only.

## Download Path Enforcement

The Server rejects module download paths that resolve outside the configured
mount directory with HTTP 403. The chart passes `storage.filesystem.mountPath` to
the Server through the `--filesystem-mount-path` flag.

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
