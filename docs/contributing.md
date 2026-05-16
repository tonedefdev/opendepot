---
tags:
  - contributing
---

# Contributing

Thank you for your interest in contributing to OpenDepot! Please read our [CONTRIBUTING.md](https://github.com/tonedefdev/opendepot/blob/main/CONTRIBUTING.md) on GitHub for guidelines on opening issues, submitting pull requests, and the development workflow.

Pull requests also run the e2e workflow, which builds the service images and scans them with Trivy before the controller tests execute. A PR fails if the scanner reports critical or high severity vulnerabilities in a built image.
