---
tags:
  - reference
  - rbac
  - ci-cd
---

# Pipeline Publisher Role

Use a dedicated ServiceAccount and least-privilege Role when a pipeline needs
to create or update `Module` resources. For the complete workflow, see
[CI/CD Pipelines](../../guides/cicd.md).

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: opendepot-ci-publisher
  namespace: opendepot-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: opendepot-module-publisher
  namespace: opendepot-system
rules:
  - apiGroups: ["opendepot.defdev.io"]
    resources: ["modules"]
    verbs: ["create", "update", "patch", "get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: opendepot-ci-publisher-binding
  namespace: opendepot-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: opendepot-module-publisher
subjects:
  - kind: ServiceAccount
    name: opendepot-ci-publisher
    namespace: opendepot-system
```

This Role does not grant permission to modify providers, versions, Secrets, or
controller resources.
