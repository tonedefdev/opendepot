---
tags:
  - quickstart
  - agents
  - skills
  - kind
  - helm
  - kubernetes
search:
  boost: 2
---

# Agents and Skills Quickstart

Run OpenDepot locally and publish Agent Skills and Agents from public GitHub branches. Then install them into a project with the `opendepot` CLI.

This Quickstart covers Agent Skills and Agents only. For modules and providers, see the [Local Quickstart](quickstart.md).

The Quickstart uses two public resources:

- The `skill-creator` skill from [anthropics/skills](https://github.com/anthropics/skills).
- The `adr-generator` agent from [github/awesome-copilot](https://github.com/github/awesome-copilot).

## Prerequisites

Install [Docker](https://docs.docker.com/get-docker/),
[Kind](https://kind.sigs.k8s.io/docs/user/quick-start/#installation),
[`kubectl`](https://kubernetes.io/docs/tasks/tools/),
[Helm 3](https://helm.sh/docs/intro/install/),
[mkcert](https://github.com/FiloSottile/mkcert#installation), and
[GPG](https://gnupg.org/download/). You install the `opendepot` CLI in Step 7.

## Step 1: Create a cluster

Create a local Kind cluster:

```bash
kind create cluster --name opendepot
kubectl config use-context kind-opendepot
kubectl create namespace opendepot-system
```

!!! note "Using an existing cluster"
    The same Helm installation works on an existing Kubernetes cluster. Skip
    the Kind commands and use the context for your cluster.

## Step 2: Create the TLS certificate

The server serves its API over HTTPS. Use [mkcert](https://github.com/FiloSottile/mkcert)
to create a local CA and a certificate for the Quickstart hostname. Run these
commands in an empty directory:

```bash
mkcert -install
mkcert -cert-file tls.crt -key-file tls.key \
  opendepot.localtest.me server.opendepot-system.svc.cluster.local localhost

kubectl create secret generic opendepot-tls \
  --from-file=tls.crt=tls.crt \
  --from-file=tls.key=tls.key \
  -n opendepot-system
```

## Step 3: Create the signing key

OpenDepot signs every Skill and Agent version. Unsigned agents aren't published,
so the version controller and the server both need a signing key.

!!! warning "Local testing only"
    This key has no passphrase and is for this Quickstart. Don't reuse it
    outside your local cluster.

Create a key without a passphrase:

```bash
gpg --batch --gen-key <<EOF
Key-Type: RSA
Key-Length: 4096
Name-Real: OpenDepot Quickstart
Name-Email: quickstart@opendepot.localtest.me
Expire-Date: 0
%no-protection
EOF
```

Export the private key as base64 and store it in a Secret:

```bash
KEY_ID=$(gpg --list-keys --with-colons quickstart@opendepot.localtest.me | awk -F: '/^pub/{print $5}' | tail -1)
PRIVATE_B64=$(gpg --armor --export-secret-keys "$KEY_ID" | base64 | tr -d '\n')

kubectl create secret generic opendepot-agent-signing \
  --from-literal=OPENDEPOT_PROVIDER_GPG_PRIVATE_KEY_BASE64="$PRIVATE_B64" \
  -n opendepot-system
```

The version controller signs each version with this key. The server loads the
same key from the Secret.

## Step 4: Install OpenDepot

Add the OpenDepot chart repository:

```bash
helm repo add opendepot https://opendepot.defdev.io
helm repo update
```

Install release 0.12.0 with the agent controller and branch sourcing enabled:

```bash
helm install opendepot opendepot/opendepot \
  --version 0.12.0 \
  --namespace opendepot-system \
  --set agent.enabled=true \
  --set branchSourcing.enabled=true \
  --set depot.enabled=true \
  --set server.anonymousAuth=true \
  --set server.tls.enabled=true \
  --set server.service.type=ClusterIP \
  --set server.gpg.secretName=opendepot-agent-signing \
  --set version.gpg.secretName=opendepot-agent-signing \
  --set storage.filesystem.enabled=true \
  --set storage.filesystem.hostPath=/var/local/opendepot-modules \
  --set scanning.enabled=true \
  --set scanning.cache.storageClassName=standard \
  --set scanning.cache.accessMode=ReadWriteOnce \
  --set monitoring.enabled=false \
  --wait
```

| Setting | Purpose |
|---------|---------|
| `agent.enabled` | Runs the agent controller, which reconciles Skill and Agent resources. |
| `branchSourcing.enabled` | Lets a Depot track a branch with `ref`. |
| `server.anonymousAuth` | Lets the CLI read the catalog and download public resources without logging in. |
| `*.gpg.secretName` | Points the server and version controller at the signing key from Step 3. |

!!! note "Monitoring is disabled"
    `monitoring.enabled=false` turns off the `ServiceMonitor`, which requires the
    Prometheus Operator CRDs. To enable monitoring later, see
    [Prometheus Monitoring](../helm-chart.md#prometheus-monitoring).

Wait for the workloads and CRDs to become ready:

```bash
kubectl get pods -n opendepot-system
kubectl get crds | grep -E 'skills|agents'
```

## Step 5: Forward the registry API

In a second terminal, forward local port 8081 to the server and keep it running:

```bash
kubectl port-forward -n opendepot-system svc/server 8081:80
```

The registry API is available at `https://opendepot.localtest.me:8081`. The
hostname resolves to `127.0.0.1`, and the mkcert certificate covers it.

## Step 6: Deploy the Depot

Save the following manifest as `agents-depot.yaml`, then apply it:

```yaml
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: agents-quickstart-depot
  namespace: opendepot-system
spec:
  pollingIntervalMinutes: 60
  skillConfigs:
    - name: skill-creator
      repoOwner: anthropics
      repoUrl: https://github.com/anthropics/skills
      path: skills/skill-creator
      ref: main
      immutable: true
      storageConfig:
        fileSystem:
          directoryPath: /data/modules
  agentConfigs:
    - name: adr-generator
      repoOwner: github
      repoUrl: https://github.com/github/awesome-copilot
      path: agents/adr-generator.agent.md
      platform: copilot
      ref: main
      immutable: true
      storageConfig:
        fileSystem:
          directoryPath: /data/modules
```

```bash
kubectl apply -f agents-depot.yaml
```

Each entry tracks a branch with `ref`. When a new commit lands on `main`, the
Depot publishes a new version. The `name` becomes the resource name in the
registry, and `path` points to the skill folder or agent file in the repository.

Check that the Skill and Agent resources are synced:

```bash
kubectl get skills.opendepot.defdev.io,agents.opendepot.defdev.io -n opendepot-system
```

The `SYNCED` column shows `true` and `LATESTVERSION` shows `1.0.0` when the first
version is ready. This can take a minute or two.

## Step 7: Install the CLI

The registry serves prebuilt `opendepot` binaries, so you don't need a Go
toolchain. Download the binary for your platform and the `SHA256SUMS` file from
the server. The steps use the port-forward from Step 5 and the mkcert
certificate from Step 2, which the system trusts after `mkcert -install`.

```bash
REGISTRY=https://opendepot.localtest.me:8081
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$(uname -m)" in
  x86_64 | amd64) ARCH=amd64 ;;
  arm64 | aarch64) ARCH=arm64 ;;
  *) echo "Unsupported architecture" >&2; exit 1 ;;
esac
BIN="opendepot_${OS}_${ARCH}"

curl -fsSL "$REGISTRY/opendepot/cli/v1/$OS/$ARCH" -o "$BIN"
curl -fsSL "$REGISTRY/opendepot/cli/v1/SHA256SUMS" -o SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing && \
  chmod +x "$BIN" && \
  sudo install -m 0755 "$BIN" /usr/local/bin/opendepot && \
  opendepot --version
```

On Linux, replace `shasum -a 256 -c` with `sha256sum -c`. The checksum check must print `OK` before the binary is installed. For Windows and other install options, see [Download the binary](../reference/cli.md#download-the-binary).

## Step 8: Browse the catalog

The CLI reads the catalog without a login because anonymous access is enabled.
List the published skills and agents:

```bash
opendepot list skills --registry opendepot.localtest.me:8081
opendepot list agents --registry opendepot.localtest.me:8081
```

Each row shows the `SOURCE` as `namespace/name`. The namespace is the
Kubernetes namespace of the resource, so both entries appear as
`opendepot-system/...`.

## Step 9: Install into a project

Create a scratch project and declare the skill and agent in `opendepot.hcl`:

```bash
mkdir agents-demo && cd agents-demo
```

```hcl
opendepot {
  registry = "opendepot.localtest.me:8081"
}

agent_skill "skill-creator" {
  source  = "opendepot.localtest.me:8081/opendepot-system/skill-creator"
  version = ">= 1.0.0"
  path    = ".github/skills/skill-creator"
}

agent "adr-generator" {
  source  = "opendepot.localtest.me:8081/opendepot-system/adr-generator"
  version = ">= 1.0.0"
  path    = ".github/agents/adr-generator.agent.md"
}
```

Preview the changes, apply them, and verify the result:

```bash
opendepot plan
opendepot apply
opendepot verify
```

`plan` shows a `+` for each target that will be created. `apply` writes the
files and `opendepot.lock.hcl`. `verify` checks the installed files against the
lock file without network access.

The first install pins the signing key that signed each package. Later installs
must match that key. To pin the key up front, add `signing_key` to the block.

## Step 10: Clean up

Remove the Kind cluster and the scratch project when you finish:

```bash
cd ..
rm -rf agents-demo agents-depot.yaml
kind delete cluster --name opendepot
```

## Next steps

- [Consume agents and skills](../guides/agents.md) to learn the CLI workflow in more detail.
- [Publish agents and skills](../guides/agents-publishing.md) from your own repositories.
- [Agent hooks](../guides/agents-hooks.md) to run `opendepot verify` automatically.
- [CLI reference](../reference/cli.md) for every command and flag.
