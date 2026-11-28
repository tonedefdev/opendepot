/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ocibuild holds the pure logic for in-cluster rootless image builds from Git
// (OCIArtifact.build). It stages a repository archive into a deterministic build context, renders the
// build Job and its token Secret, reads and validates the OCI layout that BuildKit writes, and assembles
// the multi-platform image index. It does not talk to the Kubernetes API, GitHub, or the storage backend,
// so the controller, the broker, and the helper modes can share it.
package ocibuild
