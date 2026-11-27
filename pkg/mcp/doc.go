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

// Package mcp parses and validates MCP server metadata and renders MCP client configuration for OpenDepot.
//
// It covers four things. ParseServerJSON validates server.json against schema 2025-12-11. ParseLaunchDescriptor
// validates the opendepot-mcp.json launch descriptor. The Client fetches npm packuments and PyPI project metadata
// through an allow-list with redirect and dial-time address checks. The Render functions merge OpenDepot-managed
// entries into Copilot, Claude, Codex, and custom MCP config files without changing unmanaged entries.
package mcp
