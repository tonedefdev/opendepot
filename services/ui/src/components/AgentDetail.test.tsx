// @vitest-environment jsdom

import { describe, expect, it } from "vitest";
import { buildAgentConfigSnippet } from "./AgentDetail";

describe("buildAgentConfigSnippet", () => {
  it("renders normal values exactly as before", () => {
    const snippet = buildAgentConfigSnippet({
      kind: "agent",
      namespace: "opendepot-system",
      name: "gh-actions-debug",
      registryHost: "localhost:8443",
      latestVersion: "v0.9.0",
    });

    expect(snippet).toBe(`opendepot {
  targets = ["copilot", "claude"]
}

agent "gh-actions-debug" {
  source = "localhost:8443/opendepot-system/gh-actions-debug"
  version = "0.9.0"
}
`);
  });

  it("renders the ui-demo skill without a version when none is provided", () => {
    const snippet = buildAgentConfigSnippet({
      kind: "skill",
      namespace: "opendepot-system",
      name: "ui-demo",
      registryHost: "localhost:8443",
    });

    expect(snippet).toBe(`opendepot {
  targets = ["copilot", "claude"]
}

agent_skill "ui-demo" {
  source = "localhost:8443/opendepot-system/ui-demo"
}
`);
  });

  it("escapes double quotes and HCL template openers in interpolated values", () => {
    const snippet = buildAgentConfigSnippet({
      kind: "agent",
      namespace: "acme",
      name: 'bad"name',
      registryHost: "reg${host}%{x}",
      latestVersion: 'v1."2',
    });

    expect(snippet).toContain('agent "bad\\"name" {');
    expect(snippet).toContain('source = "reg$${host}%%{x}/acme/bad\\"name"');
    expect(snippet).toContain('version = "1.\\"2"');
  });

  it("escapes backslashes so they cannot terminate or alter quoted strings", () => {
    const snippet = buildAgentConfigSnippet({
      kind: "agent",
      namespace: "acme",
      name: "reviewer",
      registryHost: "registry.example.com\\",
    });

    expect(snippet).toContain('source = "registry.example.com\\\\/acme/reviewer"');
  });
});
