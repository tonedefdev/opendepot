# Implementation Plan: In-cluster rootless image builds from Git (`OCIArtifact.build`)

Status: Q1–Q3 approved with the defaults (see [Decisions recorded](#decisions-recorded)). B0 spike complete (see [B0 Spike Results](#b0-spike-results)). Scope addition to 0.13.0. Branch base: `release-0.13.0/13-server-oci-helm` (`db3e1654`), stacked after Wave 2b-1 (PRs #107–#125).
Steps use the `B` prefix so they don't collide with release plan Steps 1–45.

## Summary

Add a third source to `OCIArtifact`, `build:`. It is valid only when `artifactType: Image`. For each Version, the version controller:

1. Resolves the Git tag to a pinned commit SHA.
2. Downloads and sanitizes the source archive itself, so GitHub credentials never leave the controller, and stages the build context as a content-addressed blob.
3. Runs one short-lived, isolated rootless BuildKit Kubernetes Job per platform in a dedicated builds namespace.

The Job pulls the context from an internal "build broker" endpoint on the version controller and pushes the resulting OCI layout back through the same broker. Each request uses a one-time per-build token. The broker writes the layout into `pkg/oci.Store` on the Version's storage.

The controller then assembles a multi-platform index and hands it to the existing `finishOCISync`. This means the same `oci_scan.go` path (`trivy image --input`, then `policy.Apply` with scan type `oci`) and the same `services/server/oci_distribution.go` serving path apply without changes.

There is no Docker daemon and no `privileged: true`, and the pod never sees cluster credentials, storage credentials, or GitHub credentials. The build mechanism lives in a reusable `pkg/ocibuild` module, so `Source` MCP servers can reuse it in 0.14.0.

## Decisions taken in this plan (confirm or override)

| # | Decision | Rationale |
|---|---|---|
| BD1 | `build:` is mutually exclusive with `mirror:` and `github:`, and is valid only for `Image`. D2 ("images can only be mirrored") becomes "images can be mirrored or built". | Additive. Existing objects still validate. |
| BD2 | Source is GitHub only. It reuses `OCIArtifactConfig.GithubClientConfig` and `pkg/github`. The controller fetches the tarball at the **commit SHA**, not at the tag. | GitHub App keys stay in the version controller, and the build is pinned to an immutable ref. |
| BD3 | Use one Job per platform, scheduled on a node of the **native** arch (`kubernetes.io/arch` nodeSelector). Don't use QEMU or binfmt. | Registering binfmt needs a privileged DaemonSet. Native builds are faster and need no extra privilege. |
| BD4 | Artifacts move over HTTP through a **build broker** in the version controller, not through a shared PVC or by giving the pod storage credentials. | Works the same on multi-node clusters, Kind, S3, GCS, Azure, and the filesystem backend. The pod gets no RWX volume and no cloud credentials. |
| BD5 | No BuildKit cache is persisted or shared between builds. Builds are cold. | Prevents cross-tenant cache poisoning. A per-OCIArtifact cache can come later. |
| BD6 | Attestations and provenance manifests are **off**. The controller writes source provenance as OCI annotations on the index it assembles. | Keeps `ociPlatforms` and Trivy input clean (no `unknown/unknown` entries). Full SLSA provenance is a follow-up. |
| BD7 | A Dockerfile `# syntax=` parser directive is rejected by default. | Otherwise any Dockerfile can pull and run an arbitrary frontend image. |
| BD8 | Reproducible output: `SOURCE_DATE_EPOCH` is the commit time, and the OCI exporter sets `rewrite-timestamp=true`. | Rebuilding the same commit gives a stable digest where the Dockerfile allows it. |
| BD9 | A synced build is never rebuilt while `immutable` is unset or true. A moved tag sets the existing `status.ociManifest.tagDrift`. `forceSync` rebuilds only when the Version is unsynced or `immutable: false`. | Same semantics as mirrors, so the server and UI need no changes. |
| BD10 | A failed build is terminal until `forceSync`. Infrastructure disruption (eviction or node drain) is retried by `podFailurePolicy` (`DisruptionTarget` → `Ignore`). | Prevents hot loops that burn cluster compute on deterministic failures. |
| BD11 | `oci.builds.requireScanning: true` (the default) refuses to build when `scanning.enabled` is false. | Untrusted builds must pass through Trivy and ScanPolicy. |
| BD12 | `MCPServer` `Source` stays `runnable: false` in 0.13.0, and no MCP code changes. The documented pattern is an `OCIArtifact` with `build:` plus an `MCPServer` of `type: Image` that references it. `pkg/ocibuild` is the seam for 0.14.0. | The MCP runtime ships in 0.14.0, so a built MCP image has no consumer in 0.13.0. The Image type already covers it. |

## Affected Services

| Area | Change |
|---|---|
| `api/v1alpha1` | `OCIBuildSource`, `OCIBuildArg`, `OCIBuildResources`, and `OCIArtifactConfig.Build`. The CEL rules are rewritten. Adds `OCIBuildStatus` and `VersionStatus.OCIBuild`. |
| `pkg/ocibuild` (new module) | Safe extraction and deterministic repacking of the context, Dockerfile pre-checks, Job rendering (the security posture lives here, in one place), tokens, OCI layout reading and validation, and multi-platform index assembly. |
| `pkg/github` | `ResolveTagCommit` and `ListTagNames` (prefix-stripped raw tags, for `tagPattern`). |
| `pkg/oci` | `Store.DeleteBlob`, used to remove the staged context blob. |
| `services/version` | `syncOCIBuild` orchestration, Job and Pod watches in the builds namespace, cleanup on deletion, the build broker HTTP server (a manager Runnable on every replica), and the `--build-helper=fetch\|upload` modes. These reuse the version-controller image, like the existing `--token-refresh-helper`. |
| `services/oci` | Gates `build:` on `--oci-builds-enabled` and the repo-owner allow-list, with the same status pattern as the registry allow-list. |
| `services/depot` | Git tag discovery for `build:` sources, as part of release Step 12. |
| `services/server` | **No change.** It serves by `status.ociManifest.digest` from the store. |
| `chart/opendepot` | `oci.builds.*` values, the builds namespace with PSS labels, a Role, NetworkPolicies, a ValidatingAdmissionPolicy, a ResourceQuota and LimitRange, the broker Service, and version and oci deployment flags. |
| CI | The e2e workflow sets `kernel.apparmor_restrict_unprivileged_userns=0` on Ubuntu 24.04 runners for the build e2e entry. |

## API / CRD Changes

### `api/v1alpha1/ociartifact_types.go`

Replace the first two `OCIArtifactConfig` XValidation rules, and add new ones:

- `rule="(has(self.mirror) ? 1 : 0) + (has(self.github) ? 1 : 0) + (has(self.build) ? 1 : 0) == 1",message="exactly one of mirror, github, or build is required"`
- `rule="!has(self.github) || self.artifactType != 'Image'",message="images can only be mirrored or built"`
- `rule="!has(self.build) || self.artifactType == 'Image'",message="build is only valid for Image"`
- `rule="!has(self.build) || !has(self.platforms) || self.platforms.all(p, p.startsWith('linux/'))",message="build platforms must be linux"`
- The `platforms` and `tagPattern` rules stay as they are.

New field on `OCIArtifactConfig`, after `Github`:

| Field | Type | JSON | Markers / doc |
|---|---|---|---|
| `Build` | `*OCIBuildSource` | `build,omitempty` | "The GitHub repository that the Image is built from in-cluster with rootless BuildKit. Only valid for Image. Requires the chart value `oci.builds.enabled`." |

`OCIBuildSource`:

| Field | Type | JSON | Markers |
|---|---|---|---|
| `RepoOwner` | `string` | `repoOwner` | `Required`, `MaxLength=39`, `Pattern=^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$` |
| `RepoUrl` | `*string` | `repoUrl,omitempty` | `MaxLength=512`. Checked at runtime with `pkg/github.ValidateRepoURL`. |
| `TagPrefix` | `*string` | `tagPrefix,omitempty` | `MaxLength=128`. Same semantics as `OCIGithubSource.TagPrefix`. |
| `ContextPath` | `string` | `contextPath,omitempty` | `+kubebuilder:default="."`, `MaxLength=1024`, `Pattern=^[A-Za-z0-9._/-]+$`, XValidation `self == '.' \|\| (!self.startsWith('/') && !self.split('/').exists(s, s == '..' \|\| s == '.'))` with message "contextPath must be a relative path without dot segments". Relative to the repository root. |
| `DockerfilePath` | `string` | `dockerfilePath,omitempty` | `+kubebuilder:default="Dockerfile"`, `MaxLength=1024`, same pattern and rule as `ContextPath` except that `.` alone isn't allowed. **Relative to the repository root**, like `docker build -f`, so a Dockerfile outside the context is supported. |
| `Target` | `*string` | `target,omitempty` | `MaxLength=128`, `Pattern=^[A-Za-z0-9][A-Za-z0-9._-]*$` |
| `BuildArgs` | `[]OCIBuildArg` | `buildArgs,omitempty` | `MaxItems=32`, `+listType=map`, `+listMapKey=name`. Doc: "Non-secret build arguments. Values are stored in plain text in the resource and the image history. Do not put secrets here." |
| `TimeoutSeconds` | `*int32` | `timeoutSeconds,omitempty` | `Minimum=60`, `Maximum=7200`. The controller clamps it to `oci.builds.maxTimeoutSeconds`. The default comes from the chart. |
| `Resources` | `*OCIBuildResources` | `resources,omitempty` | The controller clamps each value to the chart maximum. |

`OCIBuildArg`:

- `Name string` (`name`): `Required`, `MaxLength=128`, `Pattern=^[A-Za-z_][A-Za-z0-9_]*$`, and XValidation `!self.startsWith('BUILDKIT_') && self != 'SOURCE_DATE_EPOCH'` with message "reserved build argument".
- `Value string` (`value`): `MaxLength=4096`, `Pattern=^[^\x00-\x08\x0a-\x1f\x7f]*$`, which rejects control characters and newlines.

`OCIBuildResources`: `CPU`, `Memory`, and `EphemeralStorage`, each `*resource.Quantity` with `omitempty`.

Note: the Version CRD and the Depot CRD embed `OCIArtifactConfig`, so the schema and rules propagate to them. Re-check the 256 KiB CRD size limit (release Step 4).

### `api/v1alpha1/types.go` (shared)

`VersionStatus` gets `OCIBuild *OCIBuildStatus` (`ociBuild,omitempty`, `+optional`). New types:

- `OCIBuildStatus`:
  - `BuildID string` (`buildID`, `Pattern=^[a-z0-9]{20}$`).
  - `SourceCommit string` (`sourceCommit`, `Pattern=^[0-9a-f]{40}$`).
  - `SourceTag string` (`sourceTag`, `MaxLength=255`).
  - `ContextDigest string` (`contextDigest,omitempty`, the sha256 pattern).
  - `FetchTokenHash` and `UploadTokenHash string` (`omitempty`, the sha256 pattern). These are SHA-256 hashes of 256-bit random tokens, so they are safe to show in status.
  - `Phase string` (`+kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed`).
  - `StartedAt` and `CompletedAt *metav1.Time`.
  - `Message string` (`MaxLength=1024`).
  - `Platforms []OCIBuildPlatformStatus` (`MaxItems=16`).
- `OCIBuildPlatformStatus`:
  - `Platform string` (the platform pattern used on `OCIArtifactConfig.Platforms`).
  - `JobName string` (`MaxLength=63`).
  - `Phase string` (the same enum).
  - `ManifestDigest string` (`omitempty`, the sha256 pattern).
  - `Message string` (`MaxLength=1024`).

The server and UI read only `status.ociManifest`. `status.ociBuild` is for display and debugging.

## Build Execution Architecture

### Flow, per Version (`syncOCIBuild` in the version controller)

1. **Gate.** Check that builds are enabled, scanning is on (when `requireScanning` is set), the repo owner is on the allow-list, and storage is configured. A failed gate is terminal: it calls `terminalOCI`.
2. **Pin.** Build the tag from `TagCandidates(tagPrefix, version)`. Call `ResolveTagCommit` (the GitHub commits API, which dereferences annotated tags) to get a SHA, then check it with `ValidateCommitSHA`. Record `sourceCommit`, `sourceTag`, and the commit time.
3. **Stage the context** inside the controller, using `pkg/ocibuild`:
   - Download with `GetArchiveRequest(Tarball, sha)`, under a compressed limit of `maxContextBytes` (default 256 MiB).
   - Extract with bounds: decompressed size of at most 1 GiB, at most 100k entries, no absolute paths or `..` segments, no hardlinks or device files, and symlinks only when they resolve inside the repo root. Strip the top-level `<owner>-<repo>-<sha>/` directory.
   - Resolve `contextPath` (`.` means the root) and `dockerfilePath` with `CleanSourcePath`. Both must exist, and the Dockerfile must be at most 1 MiB.
   - Run the Dockerfile pre-checks: reject a `syntax` parser directive (BD7), and reject a Dockerfile with no `FROM`.
   - Repack deterministically (sorted entries, zeroed mtime, uid and gid 0) into a `tar.gz` that contains `context/**`, `dockerfile/Dockerfile`, and `dockerfile/Dockerfile.dockerignore` when the repo has one next to the Dockerfile. `.dockerignore` at the context root stays in `context/`, where BuildKit reads it.
   - Store the archive with `Store.PutBlob` in the Version's own repo, and record `contextDigest`.
   - Submodules and Git LFS objects are not included, because GitHub archives omit them. Document this.
4. **Reserve.** If active build Jobs (from the Job cache) number at least `maxConcurrentJobs`, set SyncStatus to "Waiting for a build slot" and requeue after 30 seconds.
5. **Launch.** Generate the `buildID` and the two tokens. Write `status.ociBuild` (Running, token hashes, and one platform entry with its `JobName`) **before** creating any object, so a restart can resume. Then:
   - Create one Job per platform in the builds namespace (`odb-<buildID>-<arch>[-<variant>]`).
   - Then create Secret `odb-<buildID>` with keys `fetch-token` and `upload-token`, with ownerReferences to every platform Job. The kubelet waits for the Secret volume, and garbage collection removes the Secret together with the Jobs.
6. **Observe.** Job and Pod events in the builds namespace enqueue the Version. The map function reads Job annotations; label values are too short to hold namespace and name.
   - A platform succeeds when its Job completes. Its manifest digest comes from the `upload` container's termination message and is checked with `Store.GetManifest`.
   - A platform fails on `DeadlineExceeded`, `BackoffLimitExceeded`, or a Pod stuck `Unschedulable` (for example, no node of that arch). The message is the sanitized last 1 KiB of the BuildKit container's `FallbackToLogsOnError` message, with control characters stripped.
   - One failed platform fails the whole build. The controller deletes the sibling Jobs and the build is terminal (BD10).
7. **Assemble.** For every platform manifest:
   - Check that it's an image manifest whose config platform matches, with no attestations, at most 128 layers, each blob at most `maxImageBlobBytes`, and a total of at most `maxImageBytes`.
   - Build an OCI image index (always an index, even for one platform) with `platform` descriptors and these annotations:
     - `org.opencontainers.image.source` = the repo URL
     - `org.opencontainers.image.revision` = the commit SHA
     - `org.opencontainers.image.version` = the Version tag
     - `opendepot.defdev.io/build-id`
   - Write it with `PutManifest`, then build an `oci.ResolvedManifest{Descriptor, Content, Manifests}`.
8. **Scan and serve.** Build `OCIManifestStatus`, with `UpstreamDigest` empty, `Platforms`, and `TotalSizeBytes`. Then call the existing `finishOCISync(ctx, req, version, "Image", manifest, nil, ociScanInput{root, blobs})`. This is the exact Trivy and ScanPolicy path. The server serves `status.ociManifest.digest` without changes.
9. **Clean up.** Delete the staged context blob (`Store.DeleteBlob`) and delete the Jobs (background propagation, which also garbage-collects the Secret). `ttlSecondsAfterFinished` is the backstop. On Version deletion, the existing finalizer path also deletes Jobs labelled with the build ID.
10. **Drift.** A synced build requeues every `ociDriftRequeueInterval` and re-resolves tag → SHA. A different SHA sets `tagDrift` (BD9).

### Build Job pod (rendered only by `pkg/ocibuild.RenderJob`)

Job spec:

- `backoffLimit: 0`
- `activeDeadlineSeconds` = the clamped timeout
- `ttlSecondsAfterFinished` = `oci.builds.ttlSecondsAfterFinished` (default 600)
- `podFailurePolicy`: `DisruptionTarget` → `Ignore`, and any non-zero exit code → `FailJob`
- `completions: 1` and `parallelism: 1`

Pod spec:

- `restartPolicy: Never`, `serviceAccountName: opendepot-build` (no RBAC bindings), `automountServiceAccountToken: false`, and `enableServiceLinks: false`.
- `hostNetwork`, `hostPID`, and `hostIPC` are false, and `shareProcessNamespace: false`.
- `nodeSelector`: `kubernetes.io/os: linux` and `kubernetes.io/arch: <arch>`, merged with the chart's `nodeSelector`. Tolerations, `priorityClassName`, `runtimeClassName`, and optional `hostUsers: false` come from the chart.
- Pod `securityContext`: `runAsNonRoot: true`, `runAsUser: 1000`, `runAsGroup: 1000`, `fsGroup: 1000`, and `seccompProfile: RuntimeDefault`.
- Volumes, all `emptyDir` with `sizeLimit`:
  - `workspace` (the context)
  - `output` (the OCI layout)
  - `buildkitd` (the state at `/home/user/.local/share/buildkit`; upstream requires it)
  - `rootlesskit` (`/home/user/.local/tmp`). rootlesskit writes its state directory there, and the container fails on a read-only root without this mount (B0).
  - `run` (`/run/user/1000`)
  - `tmp`
  - Plus a `secret` volume with `items` projections, so each container sees only its own key.

Containers, run in order:

| Container | Kind | Image | Security context | Mounts |
|---|---|---|---|---|
| `fetch-context` | init | version-controller image, `--build-helper=fetch` | **Restricted**: UID 65532, `allowPrivilegeEscalation: false`, drop `ALL`, `readOnlyRootFilesystem: true`, `seccompProfile: RuntimeDefault` | `fetch-token` only, `workspace` (rw) |
| `buildkit` | init | `moby/buildkit:v0.35.1-rootless@sha256:7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be`, command `buildctl-daemonless.sh` | UID and GID 1000, `runAsNonRoot: true`, **`seccompProfile: Unconfined`**, **`appArmorProfile: Unconfined`**, **`allowPrivilegeEscalation: true`** (`newuidmap` and `newgidmap` are setuid), capabilities drop `ALL` and add `SETUID` and `SETGID` (confirmed in B0, see [B0 Spike Results](#b0-spike-results)), `readOnlyRootFilesystem: true` (confirmed in B0), `procMount: Default`, `terminationMessagePolicy: FallbackToLogsOnError` | `workspace` (ro), `output`, `buildkitd`, `rootlesskit`, `run`, and `tmp`. **No token and no credentials.** |
| `upload` | main | version-controller image, `--build-helper=upload` | Restricted, as for `fetch-context` | `upload-token` only, `output` (ro) |

- `buildkit` sets the environment variable `BUILDKITD_FLAGS=--oci-worker-no-process-sandbox`. This is the upstream Kubernetes example. `buildkit` also sets `DOCKER_CONFIG=/tmp/docker-config`, because the client can't create `/home/user/.docker` on the read-only root (B0). Kubernetes has no equivalent of `systempaths=unconfined` unless `procMount: Unmasked` is used together with `hostUsers: false`.
- `buildkit` gets its arguments as an argv array, with no shell:
  - `build`, `--frontend dockerfile.v0`
  - `--local context=/workspace/context`, `--local dockerfile=/workspace/dockerfile`
  - `--opt filename=Dockerfile`, `--opt platform=<p>`, `[--opt target=<t>]`
  - `--opt build-arg:SOURCE_DATE_EPOCH=<commit epoch>`, then one `--opt build-arg:<n>=<v>` per build argument
  - `--output type=oci,dest=/output/layout,tar=false,rewrite-timestamp=true`
- Because `buildkit` is an **init container**, every build process (including anything a `RUN` step leaves in the background) has exited before `upload` starts. Each container has its own mount namespace, so `RUN` steps can't read the upload token even with `--oci-worker-no-process-sandbox`.
- Every container has CPU, memory, and ephemeral-storage requests and limits. `buildkit` uses the CR values, clamped to `oci.builds.resources.max`.

### What rootless BuildKit really needs, and how Pod Security Standards treat it

- **Restricted PSS: fails.** It needs `seccompProfile` `Unconfined`, `allowPrivilegeEscalation: true`, and added capabilities other than `NET_BIND_SERVICE`.
- **Baseline PSS: fails.** Baseline forbids `seccompProfile: Unconfined` and `appArmorProfile: Unconfined`. Adding `SETUID` and `SETGID` is allowed under Baseline, but the two unconfined profiles alone fail it.
- **So the builds namespace must be labelled `pod-security.kubernetes.io/enforce: privileged`,** with `warn: baseline` and `audit: baseline`. The pod still is **not** privileged: it has no `privileged: true`, no host namespaces, no hostPath, a non-root UID, and no service account token.
- Compensating controls, rendered by the chart and all on by default:
  1. **Dedicated namespace** `opendepot-builds`, which holds nothing but build pods. The only subject that can create Jobs, Pods, or Secrets there is the version-controller service account (a Role in that namespace only). The cluster-wide `version-controller-role` gains nothing, and `version-rbac.yaml` is unchanged.
  2. **ValidatingAdmissionPolicy** (Kubernetes ≥ 1.30) bound to that namespace. It rejects any Pod unless all of these hold:
     - The images are exactly the pinned BuildKit digest or the helper image.
     - `privileged` is false or unset everywhere.
     - No host namespaces and no hostPath.
     - Capability adds are a subset of `[SETUID, SETGID]`, and only in the `buildkit` container.
     - `seccompProfile: Unconfined` and `appArmorProfile: Unconfined` appear only on `buildkit`.
     - `runAsUser` is 1000 or 65532, and never 0.
     - `automountServiceAccountToken` is false, and the service account is `opendepot-build`.
  3. **NetworkPolicy**, described below.
  4. **ResourceQuota and LimitRange** on the namespace, as a hard ceiling under `maxConcurrentJobs`.
  5. **Optional hardening:**
     - `oci.builds.userNamespaces: true` sets `hostUsers: false`. It needs Kubernetes ≥ 1.33 with containerd ≥ 2.0 or CRI-O ≥ 1.25, and kernel ≥ 6.3 for idmapped mounts. Container UID 1000 is then mapped to an unprivileged host UID, so a runc or kernel escape lands as nobody. B0 found that on Kind v1.32.2 the API accepts `hostUsers: false`, but the pod keeps the identity UID map (`/proc/self/uid_map` is `0 0 4294967295`), so no user namespace is created. The controller must verify the mapping at runtime, or fail closed, before it relies on this option.
     - `oci.builds.runtimeClassName` (for example Kata) runs each build in a microVM.
- **Node prerequisites** to document:
  - `user.max_user_namespaces > 0`.
  - On Ubuntu 24.04+ hosts and GitHub runners, `kernel.apparmor_restrict_unprivileged_userns=0`.
  - Kernel ≥ 5.11 for the rootless overlayfs snapshotter. Otherwise set `oci.builds.snapshotter: native`.
  - On COS, the `buildkitd` `emptyDir` (always rendered).

### Network

- Rootless BuildKit always uses `network.host`, which is the **pod** network namespace. `RUN`, `ADD <url>`, and `FROM` pulls therefore all leave through the pod's egress, and the NetworkPolicy is the enforcement point. The policy needs a CNI that enforces NetworkPolicy, and B0 verified that kindnet enforces it on Kind v1.32.2 (`ipBlock` with `except`, `podSelector` egress, and deny-all; see [B0 Spike Results](#b0-spike-results)).
- Build pod policy (`opendepot.defdev.io/build: "true"`):
  - Ingress: deny all.
  - Egress:
    1. DNS to kube-dns, TCP and UDP 53.
    2. The broker Service port on version-controller pods (namespaceSelector for the release namespace plus podSelector `app: version-controller`).
    3. `ipBlock` `oci.builds.networkPolicy.egressCIDRs` (default `0.0.0.0/0` and `::/0`) **except** `exceptCIDRs`. The default except list is `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `100.64.0.0/10`, `169.254.0.0/16`, `fc00::/7`, and `fe80::/10`. This blocks the Kubernetes API, Pod and Service CIDRs, and cloud metadata, which stops SSRF into the cluster. See Q2.
- Version-controller policy: ingress on the broker port only from build pods, and ingress on the existing metrics and probe ports stays allowed. Validate this against the current ports.

### Broker (`services/version/internal/buildbroker`)

- A plain HTTP listener on `--oci-build-broker-addr` (default `:8090`), exposed by Service `version-controller-build-broker`. It runs on **every** replica (`NeedLeaderElection() == false`) and is stateless: auth state comes from Version status in the cache, and content goes to the shared storage backend.
- Routes, all under `/internal/builds/v1/{namespace}/{version}/{buildID}`:
  - `GET context` streams the staged context blob through `Store.GetBlob`, which verifies the digest.
  - `PUT blobs/{digest}` spools to `/tmp` with a limit, then calls `Store.PutBlob`, which verifies the digest.
  - `PUT manifests/{digest}` accepts at most 4 MiB, checks the media type, checks that every referenced blob exists, then calls `PutManifest`.
- Authentication is `Authorization: Bearer <token>`:
  - The path's `buildID` must equal `status.ociBuild.buildID`, and the phase must be `Running`.
  - `sha256(token)` is compared in constant time with `fetchTokenHash` or `uploadTokenHash`, according to the route.
  - Each token works only on its own routes.
- Limits: per-request body limits, at most 2 concurrent requests per build, and a per-IP rate limit. Never log tokens.
- The token's blast radius is one build's own output, which the Dockerfile author already controls. The broker can't be used to write to another Version or to read storage. Plain HTTP inside the cluster is accepted for 0.13.0. mTLS is a follow-up, recorded for Security Review in Step 45.

### Helper modes (`--build-helper=fetch|upload`, in the version-controller image)

- **`fetch`**: GETs the context, checks `contextDigest`, which is passed as an argument, and extracts it with the same safe extractor from `pkg/ocibuild` into `/workspace`.
- **`upload`**:
  - Reads `/output/layout`, which must hold exactly one image manifest for the expected platform, with no nested index and no attestations.
  - Uploads blobs, then the manifest.
  - Writes `{"manifestDigest":"sha256:…"}` to `/dev/termination-log`.

## Security Threat Model (summary for Step 45)

| Threat | Mitigation |
|---|---|
| A malicious `RUN` escapes to the node | Rootless BuildKit (UID 1000 in the pod; user namespace inside), no `privileged`, the ValidatingAdmissionPolicy pins the pod shape, an optional `hostUsers: false` or Kata. The residual risk is a kernel exploit through user namespaces. Document that operators who can't accept it should leave builds off, which is the default. |
| Theft of cluster or cloud credentials | `automountServiceAccountToken: false` on a service account with no RBAC, `enableServiceLinks: false`, no Secrets except this build's own tokens (scoped and isolated per container), and egress denies the API server CIDRs and `169.254.0.0/16`. |
| Leak of GitHub App credentials | The controller resolves and downloads by SHA. The pod only receives a sanitized context tarball. |
| Path traversal through `contextPath`, `dockerfilePath`, or archive entries | CEL patterns and dot-segment rules, `CleanSourcePath`, and the extractor rejects `..`, absolute paths, escaping symlinks, hardlinks, and devices. Checked in both the controller and the fetch helper. |
| Build-arg injection | Bounded list, name pattern, reserved `BUILDKIT_*` and `SOURCE_DATE_EPOCH`, no control characters, and argv with no shell. Documented as non-secret. |
| Arbitrary frontend (`# syntax=`) | Rejected by the pre-check (BD7). |
| Resource exhaustion | CR resources clamped to the chart maximum, emptyDir `sizeLimit`, ResourceQuota, `maxConcurrentJobs`, `activeDeadlineSeconds`, `backoffLimit: 0`, TTL, and context and image size limits enforced in the controller and the broker. |
| Forged or oversized upload | Per-build tokens bound to Version and build ID, digest-verified writes, the manifest must reference existing blobs, size and layer limits, and validation at assembly. |
| Cross-build cache poisoning | No shared cache (BD5). |
| Vulnerable or malicious image content | Mandatory scanning by default (BD11), plus `policy.Apply` with scan type `oci`. ScanExemptions work unchanged. |
| Untrusted repositories | `oci.builds.allowedRepoOwners`, where an empty list denies all (consistent with `allowedRegistries`). Checked in the oci controller and again in the version controller. |
| Untrusted log text in status | Truncated to 1 KiB, control characters stripped, and never rendered as HTML. Flag this for the UI. |

## Implementation Steps

Ordering: B0 → B1 → B2 is the critical foundation. B3, B5, and B6 can run in parallel with B1. Release Step 12 and Step 39 are prerequisites only for B12 and B15. **B13 edits `values.yaml` and `version-deployment.yaml`, so release Step 38 must run after B13 (or rebase onto it).**

**Step B0. Spike and plan amendment (no production code).**
- **Depends on:** none.
- **Files:** `.github/plans/release-0.13.0-spikes.md` (a new "OCI builds" section) and `.github/plans/release-0.13.0.md`:
  - add D13 and BD1–BD12
  - amend D2 and the Out of Scope line "In-cluster image builds for Source MCP servers"
  - add Steps B1–B16 to the wave table
  - make Step 38 depend on B13
  - add the build threat items to Step 45
- On Kind, on both an `ubuntu-24.04` runner and macOS Docker Desktop, verify:
  - (a) `moby/buildkit:v0.35.1-rootless` with the exact pod above. In particular: drop `ALL` plus add `SETUID` and `SETGID` versus the runtime default capabilities, `readOnlyRootFilesystem` with the emptyDirs, `--oci-worker-no-process-sandbox`, and which snapshotter gets picked.
  - (b) `type=oci,tar=false,rewrite-timestamp=true` output shape, with one manifest and no attestations by default in `buildctl`.
  - (c) `--opt build-arg:` with values that contain `=` and `,`.
  - (d) `# syntax=` behavior in `dockerfile.v0`, and whether the pre-check is sufficient.
  - (e) Whether kindnet enforces `ipBlock.except` and podSelector egress.
  - (f) `hostUsers: false` viability in Kind.
  - (g) The GitHub runner sysctl requirement.
  - (h) Pin the image digest.
- Record the results. Update this plan wherever a spike disproves an assumption.

**Step B1. API types and CEL. [SHARED]**
- **Depends on:** B0 (for the confirmed field set).
- **Files:** `api/v1alpha1/ociartifact_types.go`, `api/v1alpha1/types.go`, and `api/v1alpha1/types_test.go`. Update the marker assertions at lines ~519–520, and add CEL tests for each new rule and for build arg reservation.
- Add everything in [API / CRD Changes](#api--crd-changes).

**Step B2. Code generation and CRD sync. [SHARED]**
- **Depends on:** B1.
- **Files:** `api/v1alpha1/zz_generated.deepcopy.go`, the generated CRD bases that `make manifests` writes, and `chart/opendepot/crds/opendepot.defdev.io_ociartifacts.yaml`, `..._versions.yaml`, and `..._depots.yaml`.
- Re-check each CRD's size.

**Step B3. `pkg/ocibuild` module skeleton and workspace. [SHARED]**
- **Depends on:** none.
- **Files:** `pkg/ocibuild/go.mod`, `pkg/ocibuild/doc.go`, `go.work`, `go.work.sum`, `hack/check-go-work-modules.py` (if it lists modules), `services/version/go.mod`, and `services/version/go.sum`. The last two add the local `pkg/ocibuild` require and replace.

**Step B4. `pkg/ocibuild` library.**
- **Depends on:** B1, B3.
- **Files:** `pkg/ocibuild/**`. This step owns `go.mod` and `go.sum` from here on, adding `k8s.io/api`, `opencontainers/image-spec`, and `go-digest`. Do **not** add `moby/buildkit` as a dependency.
- Files and contents:
  - `context.go`: safe extraction and deterministic repacking, with the limits as options.
  - `dockerfile.go`: path resolution (`.` means the root) and the parser-directive check.
  - `job.go`: `RenderJob(JobParams) (*batchv1.Job, *corev1.Secret)`, holding the exact posture above, plus `Labels` and `Annotations` helpers.
  - `token.go`: 32-byte random tokens and their SHA-256 hashes.
  - `layout.go`: reads and validates the OCI layout for the uploader and the broker.
  - `index.go`: assembles the multi-platform index with annotations.
  - `platform.go`: maps platforms to arch and variant, and builds Job name suffixes.
- Tests:
  - Golden Job YAML that asserts every security context field.
  - Traversal, symlink, and hardlink corpus.
  - Determinism (same input gives the same digest).
  - Layout rejection cases: nested index, attestation, platform mismatch, and oversize.

**Step B5. `pkg/github` tag helpers.**
- **Depends on:** none.
- **Files:** `pkg/github/build_tags.go` and `pkg/github/build_tags_test.go`.
- `ResolveTagCommit(ctx, client, owner, repo, tag) (sha string, committedAt time.Time, err error)` validates its inputs with `ValidateRef`, `ValidateRepoName`, and `ValidateCommitSHA`.
- `ListTagNames(ctx, client, owner, repo, prefix) ([]string, error)` is paginated and prefix-stripped, with no semver filter. Depot applies `tagPattern` to its result.

**Step B6. `pkg/oci` `Store.DeleteBlob`.**
- **Depends on:** none.
- **Files:** `pkg/oci/store.go` and `pkg/oci/store_test.go`.
- Validates names and digests the same way `put` does. Deleting a missing blob is not an error.

**Step B7. Version controller build orchestration.**
- **Depends on:** B2, B4, B5, B6.
- **Files:**
  - `services/version/internal/controller/oci_build.go` and `oci_build_jobs.go` (new), with `oci_build_test.go` and `oci_build_jobs_test.go`
  - `services/version/internal/controller/oci_sync.go`: dispatch only. Add a `config.Build != nil` case, and update the default message to "exactly one of mirror, github, or build is required".
  - `services/version/internal/controller/opendepot_versions_controller.go`:
    - an `OCIBuilds OCIBuildOptions` field on `VersionReconciler`
    - Job and Pod `Watches` with `EnqueueRequestsFromMapFunc` that reads the annotations, registered only when builds are enabled
    - a deletion hook that deletes the build Jobs before the finalizer is removed
- Implements flow steps 1–10, including the exported `OCIBlobSinkFor(ctx, ns, name)` that the broker uses. It wraps `ociBlobStoreFor`.
- Tests use a fake GitHub client and envtest Jobs and Pods. They cover:
  - resuming after a restart
  - the concurrency slot
  - partial platform failure
  - a deadline
  - unschedulable
  - drift
  - immutable with `forceSync`
  - that the `finishOCISync` scan path is invoked with the assembled root

**Step B8. Build broker.**
- **Depends on:** B4, B6.
- **Files:** `services/version/internal/buildbroker/**` (new).
- An HTTP server with injected interfaces: a Version reader (the cache client) and a `BlobSink` resolver. Implements the routes, auth, and limits above.
- Tests: token mismatch, wrong phase, wrong build ID, cross-route token use, digest mismatch, oversize, a manifest that references missing blobs, and constant-time compare.

**Step B9. Build helper modes.**
- **Depends on:** B4.
- **Files:** `services/version/internal/buildhelper/**` (new).
- `fetch` and `upload`, as described above. They read tokens from files and never from the environment, and they write the termination message.
- Tests run against an `httptest` broker fake.

**Step B10. Version controller wiring.**
- **Depends on:** B7, B8, B9.
- **Files:** `services/version/cmd/main.go`, plus a flag test file next to it.
- Flags:
  - `--oci-builds-enabled`, `--oci-builds-namespace`
  - `--oci-builds-buildkit-image`, `--oci-builds-helper-image`
  - `--oci-builds-max-concurrent`, `--oci-builds-allowed-repo-owners`, `--oci-builds-default-platforms`
  - `--oci-builds-default-timeout`, `--oci-builds-max-timeout`, `--oci-builds-resources-default` and `-max` (CSV)
  - `--oci-builds-max-context-bytes`, `--oci-builds-max-image-bytes`
  - `--oci-builds-require-scanning`, `--oci-builds-user-namespaces`, `--oci-builds-runtime-class`, `--oci-builds-snapshotter`
  - `--oci-build-broker-addr`, `--oci-build-broker-url`
  - `--build-helper=fetch|upload`, which short-circuits like `--token-refresh-helper`
- Set `cache.Options.ByObject` for `batchv1.Job` and `corev1.Pod`, restricted to the builds namespace, in both cluster-wide and `WATCH_NAMESPACE` modes. Call `mgr.Add(broker)`.
- Fail fast when builds are enabled and the image or namespace is missing.

**Step B11. OCI controller gates.**
- **Depends on:** B2.
- **Files:** `services/oci/internal/controller/opendepot_ociartifacts_controller.go`, `opendepot_ociartifacts_controller_test.go`, and `services/oci/cmd/main.go`.
- `--oci-builds-enabled` and `--oci-builds-allowed-repo-owners`. When `config.Build` is set and builds are disabled, set the status "In-cluster builds are disabled". When the owner isn't on the allow-list, set "Repository owner %s is not on the build allow-list". Both match the registry allow-list pattern: Synced is false and no Versions are created.

**Step B12. Depot discovery for `build:` sources.**
- **Depends on:** release Step 12, B2, B5.
- **Files:** `services/depot/internal/controller/oci_build_discovery.go` and its test (new), and a single dispatch call in `services/depot/internal/controller/oci_discovery.go`.
- Without `tagPattern`, use `ListMatchingTags(owner, repo, tagPrefix, versionConstraints)`. With `tagPattern`, use `ListTagNames` plus RE2, where the pattern is at most 256 characters and compiled once. Uses `githubClientConfig` the same way as Skills.

**Step B13. Helm chart. [SHARED]**
- **Depends on:** release Step 6, B10, B11.
- **Files:**
  - `chart/opendepot/values.yaml`
  - `chart/opendepot/templates/_helpers.tpl`: a `fail` when `oci.builds.enabled` is true without `oci.enabled`, or when `requireScanning` is true without `scanning.enabled`
  - `templates/version-deployment.yaml`: the flags, the broker `containerPort`, and a `/tmp` sizeLimit value for upload spooling
  - `templates/oci-deployment.yaml`: the flags
  - New templates:
    - `oci-builds-namespace.yaml`
    - `oci-builds-serviceaccount.yaml`
    - `oci-builds-rbac.yaml`: a Role and RoleBinding in the builds namespace for `version-controller`. Jobs: create, get, list, watch, delete. Pods: get, list, watch. Secrets: create and delete. Events: create.
    - `oci-builds-networkpolicy.yaml`
    - `oci-builds-admissionpolicy.yaml`
    - `oci-builds-quota.yaml`
    - `version-build-broker-service.yaml`
  - Chart tests, if `chart/opendepot/tests` exists.
- `version-rbac.yaml` is unchanged.
- See [Helm Chart](#helm-chart).

**Step B14. CI wiring for the build e2e. [SHARED]**
- **Depends on:** release Step 39.
- **Files:** `.github/workflows/e2e.yaml`.
- Add a matrix entry `oci-builds`, and a step before `kind create cluster` that runs `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`, gated on that entry. Pull the BuildKit image and `kind load` it so the CI run doesn't depend on rate-limited pulls.

**Step B15. Build e2e.**
- **Depends on:** B10, B11, B13, B14, release Step 39.
- **Files:** `services/oci/test/e2e/oci_build_e2e_test.go`, `services/oci/test/e2e/values-oci-builds-e2e.yaml`, and fixtures under `services/oci/test/e2e/testdata/build/` (all new).
- See [E2E Test Changes](#e2e-test-changes).

**Step B16. Docs.**
- **Depends on:** release Step 44, B13.
- **Files:**
  - `docs/guides/oci-builds.md` (new): usage, node prerequisites, the PSS explanation, the threat model, the hardening options, and the MCP `Image` pattern
  - `docs/guides/oci-artifacts.md` (a `build:` section)
  - `docs/helm-chart.md` (the `oci.builds.*` values)
  - `docs/architecture.md` (the build flow diagram)
  - `mkdocs.yml` (navigation)

Parallel lanes after B2:
- B4 → (B7 ∥ B8 ∥ B9) → B10 → B13 → B15
- B11
- B12, after Step 12

Each lane can stack after Wave 2b-1 and run alongside Wave 2b-2.

## E2E Test Changes

`services/oci/test/e2e/oci_build_e2e_test.go` runs on an isolated Kind cluster through the `opendepot-testing` skill with `E2E_FOCUS=oci-builds`. It uses the GitHub App secret pattern from the module e2e. The fixture repository and tag are listed under Assumptions. The scenarios:

1. **Happy path.** Build a multi-stage Dockerfile with `target`, a `buildArg`, and a non-root `contextPath` with `dockerfilePath` outside it, for the node's native platform. The Version syncs. Check:
   - `status.ociBuild.sourceCommit` equals the tag's SHA.
   - The index annotations hold the revision.
   - `crane pull` by digest from `/v2/` works, and the image config contains the expected ARG and label.
   - `status.ociScan` is populated.
   - The Job, the Secret, and the context blob are gone.
2. **Reproducibility.** `forceSync` with `immutable: false` on the same commit gives the same digest.
3. **Scan gate.** A fixture with a known-vulnerable base and a blocking ScanPolicy leaves the Version unsynced, with the policy message.
4. **Network isolation.** The fixture `RUN` tries the Kubernetes API Service IP and `169.254.169.254` with a short timeout and writes the result to a file. The test reads that file from the served image and asserts both were blocked. If B0 finds that kindnet doesn't enforce NetworkPolicy, the e2e installs Calico.
5. **Limits.** `RUN sleep 600` with `timeoutSeconds: 60` fails with a deadline message, and `backoffLimit: 0` is honored (one Pod). A memory hog under its limit fails with OOMKilled.
6. **Rejections:**
   - `contextPath: ../x` is rejected by CEL.
   - A `# syntax=` Dockerfile is terminal.
   - A repo owner not on the allow-list sets the oci controller status.
   - `build:` on `Helm` is rejected by CEL.
   - `build:` with `oci.builds.enabled: false` sets the disabled status.
7. **Admission.** As cluster admin, applying a Pod with `privileged: true` or a different image in `opendepot-builds` is denied by the ValidatingAdmissionPolicy. A build Pod has no service account token mount.
8. **Broker auth.** From a pod in the builds namespace, a PUT with a wrong token or a stale build ID returns 401 or 404.
9. **Unschedulable platform.** A platform with no matching node arch fails within the deadline, with a "no schedulable node" message.

Depot discovery for `build:` goes in a new file next to release Step 42: `services/depot/test/e2e/oci_build_discovery_e2e_test.go`. It is part of B12's follow-up, or it can be folded into Step 42 if that hasn't started.

## Helm Chart

| Value | Default | Purpose |
|---|---|---|
| `oci.builds.enabled` | `false` | Opt-in gate for everything below. Requires `oci.enabled`. |
| `oci.builds.namespace` / `createNamespace` | `opendepot-builds` / `true` | Dedicated namespace, labelled PSS `enforce: privileged`, `warn: baseline`, `audit: baseline` |
| `oci.builds.buildkitImage` | `moby/buildkit:v0.35.1-rootless@sha256:<B0>` | Pinned by digest. Mirror it for air-gapped clusters. |
| `oci.builds.helperImage` | `""` (the version-controller image) | `fetch` and `upload` helpers |
| `oci.builds.allowedRepoOwners` | `[]` | GitHub owners that can be built. Empty denies all. |
| `oci.builds.requireScanning` | `true` | BD11 |
| `oci.builds.maxConcurrentJobs` | `2` | Controller-side slots |
| `oci.builds.defaultPlatforms` | `[linux/amd64]` | Used when `platforms` is unset |
| `oci.builds.timeoutSeconds.default` / `.max` | `1800` / `7200` | Clamp for `activeDeadlineSeconds` |
| `oci.builds.ttlSecondsAfterFinished` | `600` | Backstop cleanup |
| `oci.builds.resources.default` / `.max` | cpu `1`/`4`, memory `2Gi`/`8Gi`, ephemeral `10Gi`/`20Gi` | Clamp for CR values. Also sizes emptyDir `sizeLimit`. |
| `oci.builds.maxContextBytes` / `maxImageBytes` | `256Mi` / `8Gi` | Controller and broker limits |
| `oci.builds.snapshotter` | `auto` | `auto`, `overlayfs`, or `native` |
| `oci.builds.userNamespaces` | `false` | Sets `hostUsers: false` |
| `oci.builds.runtimeClassName`, `nodeSelector`, `tolerations`, `priorityClassName` | unset | Placement and sandboxing |
| `oci.builds.broker.port` | `8090` | Broker listener and Service |
| `oci.builds.networkPolicy.enabled` | `true` | Build pod egress and broker ingress |
| `oci.builds.networkPolicy.egressCIDRs` / `exceptCIDRs` | `[0.0.0.0/0, ::/0]` / the private, CGNAT, link-local, and ULA ranges | See Q2 |
| `oci.builds.admissionPolicy.enabled` | `true` | ValidatingAdmissionPolicy plus its binding. Needs Kubernetes ≥ 1.30. |
| `oci.builds.resourceQuota.enabled` and its limits | `true` | Hard ceiling |

RBAC:
- A **Role in the builds namespace only** for the `version-controller` service account. Jobs: create, get, list, watch, delete. Pods: get, list, watch. Secrets: create and delete. Events: create.
- Service account `opendepot-build`, with `automountServiceAccountToken: false` and no bindings.
- `version-rbac.yaml` and `oci-rbac.yaml` are unchanged.

## Assumptions (no answer needed unless you disagree)

- **e2e fixture:** a dedicated repo `tonedefdev/opendepot-e2e-fixtures`, with tags `oci-build/v1.0.0` and `oci-build/v1.0.1` holding the multi-stage, vulnerable, network-probe, and sleep Dockerfiles. This keeps fixture tags out of the main repository.
- **Supported cluster floor for builds:** Kubernetes ≥ 1.30, for the `appArmorProfile` field and ValidatingAdmissionPolicy GA. The rest of OpenDepot is unaffected.
- The base-image allow-list is **deferred** unless Q2 says otherwise. Hostname egress can't be enforced with NetworkPolicy, and `RUN curl` can fetch anything, so a `FROM` check would be policy, not a boundary. Trivy and ScanPolicy are the real gate.

## Open Questions

1. **PSS posture.** Do you accept the default of a dedicated `opendepot-builds` namespace at PSS `enforce: privileged`, protected by the pinned-shape ValidatingAdmissionPolicy? The alternative is to **require** `hostUsers: false` (Kubernetes ≥ 1.33, containerd ≥ 2.0, kernel ≥ 6.3), which shrinks the escape impact but narrows the supported clusters and may not run on Kind in CI. Note that neither option gets the pod under Baseline, because of the unconfined seccomp and AppArmor profiles.
2. **Default build egress.** One option is (a) **internet but not the cluster**, which is the default above: most Dockerfiles that use `apt`, `npm`, or `go mod` work, and the Kubernetes API, Pod and Service CIDRs, and cloud metadata are blocked. The other is (b) **deny all** except DNS and the broker, with operators listing CIDRs, which is safer but breaks most real Dockerfiles out of the box. Also: should the best-effort `FROM` and `COPY --from` base-registry allow-list ship in 0.13.0, or be deferred?
3. **MCP `Source` reuse.** Do you confirm BD12: no MCP code in 0.13.0, `runnable: false` stays, the documented path is `OCIArtifact.build` plus `MCPServer type: Image`, and `pkg/ocibuild` is reused in 0.14.0 when the runtime lands?

## Decisions recorded

User approval, 2026-11-27: all three defaults are approved.

- **Q1 (PSS posture): approved.** The namespace `opendepot-builds` is labelled `pod-security.kubernetes.io/enforce: privileged`, with `warn` and `audit` at `baseline`. The pinned-shape ValidatingAdmissionPolicy is the control. `hostUsers: false` stays an optional hardening setting (`oci.builds.userNamespaces`), not a requirement, because B0 showed it isn't effective on Kind v1.32.2 (see below).
- **Q2 (default build egress): approved as option (a), internet but not the cluster.** The default `egressCIDRs` are `0.0.0.0/0` and `::/0`, with the default except list. The best-effort base-registry allow-list for `FROM` and `COPY --from` is **deferred**, per the Assumptions section.
- **Q3 (MCP `Source` reuse): approved.** BD12 stands. There is no MCP code in 0.13.0, `runnable: false` stays, and the documented path is `OCIArtifact` with `build:` plus an `MCPServer` of `type: Image`.

## B0 Spike Results

Run on 2026-11-27 against an isolated Kind cluster, `od-b0-spike` (`kindest/node:v1.32.2`, Kind v0.26.0). The host was macOS Docker Desktop (Docker 20.10.21, arm64, linuxkit kernel 5.15.49). The cluster was deleted afterward. The shared `opendepot` cluster was not touched.

**Not verified in this spike:** the `ubuntu-24.04` amd64 runner, so AppArmor, the `apparmor_restrict_unprivileged_userns` sysctl, and the amd64 image were not run live. The `# syntax=` pre-check (plan item d) and the `hostUsers: false` feature gate were not tested either. See Follow-ups.

### Pinned BuildKit image

| Item | Value |
|---|---|
| Tag | `moby/buildkit:v0.35.1-rootless`, the newest published rootless release. Newer rootless tags are nightly or master only. |
| Digest (multi-arch index) | `sha256:7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be` |
| Pin | `moby/buildkit:v0.35.1-rootless@sha256:7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be` |
| Index platforms | linux/amd64, linux/arm64, linux/arm/v7, linux/ppc64le, linux/riscv64, linux/s390x, plus attestation manifests (`unknown/unknown`) |

Trivy 0.70.0, `trivy image --scanners vuln`, all severities. The counts are identical for linux/amd64 and linux/arm64:

| CRITICAL | HIGH | MEDIUM | LOW | UNKNOWN | Fixed version listed |
|---|---|---|---|---|---|
| 0 | 95 | 88 | 5 | 8 | all 196 |

The findings are in the upstream binaries: Go stdlib `v1.25.8`, `golang.org/x/net` `v0.55.0`, and `github.com/moby/go-archive` `v0.2.0`. No newer published rootless tag carries those fixes. Under the Standing Decisions, the upstream image isn't patched or rebuilt, and no `.trivyignore.yaml` entry is added. **Open for Security Review:** until upstream publishes a fixed rootless release, the build image carries 95 HIGH findings. Track it as a residual risk, or decide otherwise.

### Pod posture (verified live)

| Setting | Result on Kind v1.32.2 |
|---|---|
| `capabilities.drop: [ALL]` and `add: [SETUID, SETGID]` | **Required and sufficient.** Dropping the adds fails with `newuidmap: operation not permitted`. |
| `allowPrivilegeEscalation: true` | **Required.** With `false`, `newuidmap` fails with `Could not set caps`. |
| `seccompProfile: Unconfined` (buildkit only) | **Required.** With `RuntimeDefault`, rootlesskit fails with `fork/exec /proc/self/exe: operation not permitted`. |
| `appArmorProfile: Unconfined` (buildkit only) | **Required on hosts without AppArmor.** With `RuntimeDefault`, the kubelet reports `Cannot enforce AppArmor: AppArmor is not enabled on the host`, and the container never starts. Not tested on Ubuntu runners. |
| `readOnlyRootFilesystem: true` | **Works**, with these writable mounts. Each was added after a failure: `/home/user/.local/share/buildkit` (buildkitd state), `/home/user/.local/tmp` (rootlesskit state), `/run/user/1000` (socket), `/tmp`, and `/output`. `/home/user/.docker` isn't writable, so `DOCKER_CONFIG=/tmp/docker-config` is set. |
| `BUILDKITD_FLAGS=--oci-worker-no-process-sandbox` | Works. The build passes. |
| Pod `securityContext`: UID and GID 1000, pod seccomp `RuntimeDefault` | Works. |
| Snapshotter `auto` (default) | Resolves to **overlayfs** (state directory `runc-overlayfs`). |
| Snapshotter `native` (`--oci-worker-snapshotter=native`) | Works (state directory `runc-native`). The fallback is valid. |
| Build under internet-only egress (see Network) | Works. The build pulled `busybox` from Docker Hub and completed. |
| `hostUsers: false` | The API accepts it and the pod runs, but **no user namespace is created**. `/proc/self/uid_map` is `0 0 4294967295`, the identity map. This setting is not active on this Kind node. |
| Pod Security warnings | `enforce: privileged` admits the pod. `warn: baseline` flags only the buildkit container, for `Unconfined` seccomp and AppArmor. The restricted-shaped `verify` container produced no warnings. |

### OCI output (`--output type=oci,dest=/output/layout,tar=false,rewrite-timestamp=true`)

The output is a **directory**, not a tarball:

- `oci-layout` holds `{"imageLayoutVersion":"1.0.0"}`.
- `index.json` is an OCI index (`schemaVersion: 2`) with **one** manifest entry, and no nested index. The entry has the `platform` from `platform=`, and the annotations `org.opencontainers.image.created` (equal to `SOURCE_DATE_EPOCH`) and `org.opencontainers.image.ref.name: latest`.
- `blobs/sha256/` holds the manifest, the config, and the layer. No attestation manifests appear by default, because no `attest:` options were passed.
- A leftover `ingest/` directory is also created. The upload helper must read only blobs that `index.json` references.
- `--opt build-arg:SPIKE_ARG=a=b,c` arrived intact (`RUN echo` produced `built:a=b,c`).
- `build-arg:SOURCE_DATE_EPOCH` is honored, and `rewrite-timestamp=true` set `created` to the epoch time.
- The `failed check for fsverity support ... operation not supported` warning is harmless.

### ValidatingAdmissionPolicy (verified live)

`admissionregistration.k8s.io/v1` ValidatingAdmissionPolicy and binding are enforced on Kind v1.32.2 with no feature gate. The spike policy was bound to `opendepot-builds`. Server-side dry-runs:

| Case | Pod | Result |
|---|---|---|
| A | Exact BuildKit pod shape, in `opendepot-builds` | Admitted |
| B | Same, with `privileged: true` on buildkit | Denied: `privileged containers are not allowed in opendepot-builds` |
| C | Privileged pod in `default` (binding scope) | Admitted |
| D | `hostPath` volume in `opendepot-builds` | Denied |
| E | `NET_ADMIN` capability add | Denied: `capability adds are limited to SETUID and SETGID` |
| F | Image not on the allow-list | Denied |

Because `enforce: privileged` admits privileged pods, the VAP is the only control that rejects them.

### NetworkPolicy (verified live: kindnet enforces it)

**Correction to the plan:** the assumption that kindnet does not enforce NetworkPolicy is wrong. kindnet (`docker.io/kindest/kindnetd:v20250214-acbabc1a`) enforced egress policy on Kind v1.32.2. Test pod: labelled `opendepot.defdev.io/build: "true"` in `opendepot-builds`. Target: a pod in `default`, `10.244.0.13:8080`.

| Egress policy on the build pod | Target reachable? | Other checks |
|---|---|---|
| None | Yes | |
| `ipBlock 0.0.0.0/0`, no `except` | Yes | |
| `ipBlock 0.0.0.0/0` except `10/8`, `172.16/12`, `192.168/16`, `100.64/10`, `169.254/16`, plus DNS to kube-dns | **No** | `nslookup kubernetes.default.svc.cluster.local` succeeded; a full build under this policy succeeded |
| Deny all | **No** | `1.1.1.1` unreachable |
| `podSelector` allow to `default` and `run=np-target` on TCP 8080 only | Yes | `1.1.1.1` unreachable |
| All policies removed | Yes | |

Ingress rules were not tested.

### Plan adjustments for B1–B13

1. **buildkit container mounts:** add a `rootlesskit` emptyDir at `/home/user/.local/tmp`. Set `DOCKER_CONFIG=/tmp/docker-config`. Update the volume list to match.
2. **Image pin:** use `moby/buildkit:v0.35.1-rootless@sha256:7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be` in `RenderJob`. The Trivy findings above go to Security Review.
3. **Snapshotter:** keep `auto`, which resolves to overlayfs on kernel 5.15. `oci.builds.snapshotter: native` is a verified fallback.
4. **Upload helper:** read the layout as a directory. Ignore `ingest/`. Expect exactly one manifest, with no attestations. When the index is assembled, ignore or strip `org.opencontainers.image.ref.name`.
5. **`userNamespaces: true`:** the controller must check `/proc/self/uid_map` at runtime, or fail closed. Accepting the `hostUsers` field doesn't prove the setting is active.
6. **ValidatingAdmissionPolicy:** the production policy's image allow-list must cover the pinned BuildKit digest and the version-controller helper digest. The spike policy's `busybox` entry was for the test pod only. The rules that only buildkit may add capabilities or use `Unconfined` profiles are confirmed.
7. **NetworkPolicy E2E:** B14 and B15 can assert enforcement on Kind, not skip it. Ingress is still untested, so B14 must test ingress separately.
8. **Chart:** the restricted fetch and upload containers produce no Pod Security warnings. Only buildkit needs the `privileged` namespace label.

### Follow-ups (not run)

- **Ubuntu 24.04 amd64 runner.** Confirm `appArmorProfile: Unconfined` with `kernel.apparmor_restrict_unprivileged_userns=0`, the amd64 image, and overlayfs. Also confirm the GitHub runner sysctl requirement.
- **`# syntax=` pre-check** (plan item d). Not run.
- **`hostUsers: false` with the kubelet feature gate enabled.** Not run. Until then, `userNamespaces` stays off by default.
- **Kind image loading on this host.** `kind load docker-image` failed on Docker 20.10 with `failed to detect containerd snapshotter`, even though the node's containerd was healthy. `docker save` followed by `ctr -n k8s.io images import --snapshotter=overlayfs` worked. This is environment only. The e2e harness may need the same workaround on Docker Desktop.
