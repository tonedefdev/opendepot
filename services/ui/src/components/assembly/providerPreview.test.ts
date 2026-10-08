import { describe, expect, it } from "vitest";
import type { ProviderSchemaBlock } from "@/lib/api";
import type { ProviderConfiguration, ReferenceOption } from "./types";
import { renderProviderPreview } from "./providerPreview";

describe("renderProviderPreview", () => {
  it("renders aliases, typed attributes, and nested blocks", () => {
    const schema: ProviderSchemaBlock = {
      attributes: {
        enabled: { type: "bool", optional: true },
        retries: { type: "number", optional: true },
        region: { type: "string", optional: true },
      },
      blocks: {
        assume_role: {
          nesting: "list",
          block: { attributes: { role_arn: { type: "string", required: true } } },
        },
      },
    };
    const configuration: ProviderConfiguration = {
      arguments: {
        enabled: { mode: "literal", literal: "true" },
        retries: { mode: "literal", literal: "3" },
        region: { mode: "literal", literal: "us\"west-2" },
      },
      blocks: {
        assume_role: [{ arguments: { role_arn: { mode: "literal", literal: "arn:aws:iam::123:role/deploy" } }, blocks: {} }],
      },
    };

    expect(renderProviderPreview("aws", "west", schema, configuration, [])).toBe([
      'provider "aws" {',
      '  alias   = "west"',
      '  enabled = true',
      '  region  = "us\\"west-2"',
      '  retries = 3',
      '',
      '  assume_role {',
      '    role_arn = "arn:aws:iam::123:role/deploy"',
      '  }',
      '}',
    ].join("\n"));
  });

  it("renders references and preserves recognized HCL expressions", () => {
    const schema: ProviderSchemaBlock = {
      attributes: {
        region: { type: "string", optional: true },
        profile: { type: "string", optional: true },
      },
    };
    const configuration: ProviderConfiguration = {
      arguments: {
        region: { mode: "literal", literal: 'replace(var.region, "_", "-")' },
        profile: { mode: "reference", literal: "", refNodeId: "variable-profile", refOutput: "" },
      },
      blocks: {},
    };
    const references: ReferenceOption[] = [{
      nodeId: "variable-profile",
      instanceName: "profile",
      output: "",
      type: "string",
      label: "var.profile",
      sourceMultiplicity: "none",
      sourceKind: "variable",
    }];

    expect(renderProviderPreview("aws", "", schema, configuration, references)).toBe([
      'provider "aws" {',
      '  profile = var.profile',
      '  region  = replace(var.region, "_", "-")',
      '}',
    ].join("\n"));
  });

  it("omits unset fields and still renders an empty provider block", () => {
    const schema: ProviderSchemaBlock = { attributes: { token: { type: "string", optional: true } } };
    const configuration: ProviderConfiguration = { arguments: {}, blocks: {} };

    expect(renderProviderPreview("example", "", schema, configuration, [])).toBe('provider "example" {}');
  });
});