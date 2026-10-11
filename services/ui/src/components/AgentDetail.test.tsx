// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import * as React from "react";
import { afterEach, describe, expect, it } from "vitest";
import { AgentBody, AgentDefinition, buildAgentConfigSnippet, splitFrontmatter } from "./AgentDetail";

afterEach(() => {
  cleanup();
});

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

describe("splitFrontmatter", () => {
  it("splits the frontmatter from the body", () => {
    const { frontmatter, body } = splitFrontmatter("---\nname: review\n---\n# Title\n");

    expect(frontmatter).toBe("name: review");
    expect(body).toBe("# Title\n");
  });

  it("returns the whole content as the body when there is no frontmatter", () => {
    const { frontmatter, body } = splitFrontmatter("# Title\n\nText");

    expect(frontmatter).toBeNull();
    expect(body).toBe("# Title\n\nText");
  });
});

describe("AgentDefinition", () => {
  it("shows the frontmatter in a code block and the body as markdown", () => {
    render(<AgentDefinition content={"---\nname: review\n---\n# Heading\n\nSome **bold** text"} fileName="code-review.agent.md" title="AGENT.md" />);

    expect(screen.getByLabelText("code-review.agent.md frontmatter").textContent).toBe("name: review");
    expect(screen.getByRole("heading", { name: "Heading" })).toBeTruthy();
    expect(screen.getByText("bold").tagName).toBe("STRONG");
  });

  it("renders the frontmatter with YAML syntax highlighting", () => {
    render(<AgentDefinition content={"---\nname: review\n---\n# Heading"} fileName="code-review.agent.md" title="AGENT.md" />);

    const frontmatter = screen.getByLabelText("code-review.agent.md frontmatter");
    expect(frontmatter.querySelector(".token.key")?.textContent).toBe("name");
  });

  it("renders content with no frontmatter as markdown only", () => {
    render(<AgentDefinition content={"# Only body"} fileName="SKILL.md" title="SKILL.md" />);

    expect(screen.queryByLabelText("SKILL.md frontmatter")).toBeNull();
    expect(screen.getByRole("heading", { name: "Only body" })).toBeTruthy();
  });
});

describe("AgentBody", () => {
  it.each([
    ["bash", "echo hello"],
    ["sh", "echo hello"],
    ["go", "package main"],
    ["python", "def main(): pass"],
    ["typescript", "const x: number = 1;"],
    ["ts", "const x: number = 1;"],
  ])("highlights fenced %s code blocks", (lang, code) => {
    const { container } = render(<AgentBody content={"```" + lang + "\n" + code + "\n```"} />);

    expect(container.querySelector(".token")).toBeTruthy();
    expect(container.querySelector("pre.md-plain-pre")).toBeNull();
  });

  it("keeps blocks with an unknown language as plain code", () => {
    const { container } = render(<AgentBody content={"```text\nplain output\n```"} />);

    expect(container.querySelector("pre.md-plain-pre")?.textContent?.trim()).toBe("plain output");
    expect(container.querySelector(".token")).toBeNull();
  });

  it("renders fenced markdown as markdown instead of code", () => {
    const { container } = render(
      <AgentBody content={"## Outer\n\n```markdown\n## Checklist\n\n- item one\n```"} />,
    );

    expect(container.querySelector("h2")?.textContent).toBe("Outer");
    expect(screen.getByRole("heading", { level: 2, name: "Checklist" })).toBeTruthy();
    expect(container.querySelector("li")?.textContent).toBe("item one");
    expect(container.querySelector("pre")).toBeNull();
  });
});
