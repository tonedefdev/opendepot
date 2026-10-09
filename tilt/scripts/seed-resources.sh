#!/bin/sh
set -eu

jev_block=""
if [ -n "${OPENDEPOT_JEV_API_KEY:-}" ]; then
  jev_block='    jevSecretRef:
      name: opendepot-jev
      key: jevToken
    jevPolicy: {}'
fi

kubectl apply -f - <<'EOF'
apiVersion: opendepot.defdev.io/v1alpha1
kind: Depot
metadata:
  name: ui-demo-depot
  namespace: opendepot-system
spec:
  global:
    moduleConfig:
      fileFormat: zip
      immutable: true
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
  moduleConfigs:
  - name: terraform-aws-acm
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-acm
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-api-gateway
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-api-gateway
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-cloudfront
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-cloudfront
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-ecr
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-ecr
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-iam
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-iam
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-lambda
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-lambda
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-s3
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-s3
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-secrets-manager
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-secrets-manager
    versionConstraints: '>= 0.0.0'
  - name: terraform-aws-ses
    provider: aws
    repoOwner: defdevio
    repoUrl: https://github.com/defdevio/terraform-aws-ses
    versionConstraints: '>= 0.0.0'
  pollingIntervalMinutes: 60
  providerConfigs:
  - architectures:
    - arm64
    name: aws
    operatingSystems:
    - darwin
    - linux
    versionConstraints: ~> 6.60.0
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: GroupBinding
metadata:
  name: local-test-access
  namespace: opendepot-system
spec:
  expression: '"local-test-group" in groups'
  moduleResources:
    - '*'
  providerResources:
    - '*'
  skillResources:
    - '*'
  agentResources:
    - '*'
EOF

kubectl apply -f - <<EOF
apiVersion: opendepot.defdev.io/v1alpha1
kind: Skill
metadata:
  name: gh-actions-debug
  namespace: opendepot-system
spec:
  agentSourceConfig:
    name: gh-actions-debug
    repoOwner: tonedefdev
    repoUrl: https://github.com/tonedefdev/opendepot
    path: .github/skills/gh-actions-debug
    platform: copilot
    versionConstraints: "~> 0.9.0"
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
${jev_block}
  versions:
    - version: "0.9.0"
---
apiVersion: opendepot.defdev.io/v1alpha1
kind: Agent
metadata:
  name: code-review
  namespace: opendepot-system
spec:
  agentSourceConfig:
    name: code-review
    repoOwner: tonedefdev
    repoUrl: https://github.com/tonedefdev/opendepot
    path: .github/agents
    platform: copilot
    versionConstraints: "~> 0.9.0"
    storageConfig:
      fileSystem:
        directoryPath: /data/modules
${jev_block}
  versions:
    - version: "0.9.0"
EOF