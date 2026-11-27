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

// Package agentassembly models agent assemblies: typed graphs of agents, skills, and MCP servers.
//
// Document is the graph. Digest returns its canonical sha256 digest. Validate checks the graph against the
// validation rules and their stable codes, and ValidateExport also blocks placeholder nodes. Render writes the
// platform agent files for Copilot, Claude, and agents-md, and RenderHCL writes the pinned opendepot.hcl. Route is the
// deny-by-default runtime policy that enforces delegates and retry links from an AssemblyManifest.
package agentassembly
