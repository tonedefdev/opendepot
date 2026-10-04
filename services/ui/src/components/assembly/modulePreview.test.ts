import { describe, expect, it } from "vitest";
import type { ModulePreviewInput } from "./modulePreview";
import { renderModulePreview } from "./modulePreview";
import type { ProviderOption, ReferenceOption, VariableOption } from "./types";

const referenceOptions: ReferenceOption[] = [
  {
    nodeId: "module-network",
    instanceName: "network",
    output: "subnet_id",
    type: "string" as const,
    label: "module.network.subnet_id",
    sourceMultiplicity: "none" as const,
    sourceKind: "module" as const,
  },
  {
    nodeId: "module-roles",
    instanceName: "roles_source",
    output: "roles",
    type: ["map", ["object", { name: "string" }]],
    label: "module.roles_source.roles",
    sourceMultiplicity: "none" as const,
    sourceKind: "module" as const,
  },
  {
    nodeId: "variable-lambdas",
    instanceName: "lambda_functions",
    output: "",
    type: ["map", ["object", { spec: ["object", { description: "string" }] }]],
    label: "var.lambda_functions",
    sourceMultiplicity: "none" as const,
    sourceKind: "variable" as const,
  },
];
const variableOptions: VariableOption[] = [{ nodeId: "variable-count", name: "enabled", type: { kind: "bool" } }];
const providerOptions: ProviderOption[] = [{
  nodeId: "provider-aws",
  localName: "aws",
  alias: "primary",
  providerNamespace: "hashicorp",
  providerName: "aws",
  label: "aws.primary",
}];

describe("module code preview", () => {
  it("renders a version-pinned module call with configured nested values", () => {
    const inputs: ModulePreviewInput[] = [
      {
        name: "settings",
        type: {
          kind: "object",
          attributes: [
            { name: "enabled", type: { kind: "bool" } },
            { name: "name", type: { kind: "string" } },
          ],
        },
        value: {
          kind: "object",
          entries: [
            { name: "enabled", value: { kind: "scalar", value: { mode: "literal", literal: "true" } } },
            { name: "name", value: { kind: "scalar", value: { mode: "literal", literal: "worker" } } },
          ],
        },
      },
      { name: "unused", type: { kind: "string" } },
    ];

    expect(renderModulePreview({
      instanceName: "lambda",
      namespace: "platform",
      moduleName: "terraform-aws-lambda",
      system: "aws",
      version: "v1.2.3",
      registryHost: "registry.example.com",
      inputs,
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
    })).toBe(`module "lambda" {
  source  = "registry.example.com/platform/terraform-aws-lambda/aws"
  version = "1.2.3"

  settings = {
    enabled = true
    name    = "worker"
  }
}`);
  });

  it("omits stored optional object values when their visibility is disabled", () => {
    const rendered = renderModulePreview({
      instanceName: "worker",
      namespace: "platform",
      moduleName: "worker",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [{
        name: "settings",
        type: {
          kind: "object",
          attributes: [
            { name: "environment", type: { kind: "string" } },
            { name: "labels", type: { kind: "map", element: { kind: "string" } }, optional: true },
          ],
        },
        value: {
          kind: "object",
          entries: [
            { name: "environment", value: { kind: "scalar", value: { mode: "literal", literal: "production" } } },
            { name: "labels", value: { kind: "map", entries: [{ key: "owner", value: { kind: "scalar", value: { mode: "literal", literal: "platform" } } }] } },
          ],
        },
      }],
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
      optionalFieldVisibility: { ["variable%3Asettings"]: false },
    });

    expect(rendered).toContain('environment = "production"');
    expect(rendered).not.toContain("labels");
  });

  it("renders module references as expressions rather than quoted strings", () => {
    expect(renderModulePreview({
      instanceName: "consumer",
      namespace: "platform",
      moduleName: "consumer",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [{
        name: "subnet_id",
        type: { kind: "string" },
        value: { kind: "scalar", value: { mode: "reference", literal: "", refNodeId: "module-network", refOutput: "subnet_id" } },
      }],
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
    })).toContain('  subnet_id = module.network.subnet_id');
  });

  it("renders whole complex module references as expressions", () => {
    expect(renderModulePreview({
      instanceName: "consumer",
      namespace: "platform",
      moduleName: "consumer",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [{
        name: "roles",
        type: { kind: "map", element: { kind: "object", attributes: [{ name: "name", type: { kind: "string" } }] } },
        value: {
          kind: "scalar",
          value: { mode: "reference", literal: "", refNodeId: "module-roles", refOutput: "roles", refOutputSelector: { kind: "all" } },
        },
      }],
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
    })).toContain("  roles = module.roles_source.roles");
  });

  it("renders a selected map descendant after the entry key", () => {
    expect(renderModulePreview({
      instanceName: "consumer",
      namespace: "platform",
      moduleName: "consumer",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [{
        name: "roles",
        type: { kind: "object", attributes: [{ name: "description", type: { kind: "string" } }] },
        value: {
          kind: "scalar",
          value: {
            mode: "reference",
            literal: "",
            refNodeId: "variable-lambdas",
            refOutput: "",
            refOutputSelector: { kind: "key", expr: { mode: "literal", literal: "each.key" } },
            refAttributePath: "spec",
          },
        },
      }],
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
    })).toContain("  roles = var.lambda_functions[each.key].spec");
  });

  it("renders heredoc module inputs as indented HCL expressions", () => {
    const rendered = renderModulePreview({
      instanceName: "worker",
      namespace: "platform",
      moduleName: "worker",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [{
        name: "account_id",
        type: { kind: "string" },
        value: { kind: "scalar", value: { mode: "literal", literal: "<<-EOT\n123456789012\nEOT" } },
      }],
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
    });

    expect(rendered).toContain("  account_id = <<-EOT\n  123456789012\n  EOT");
  });

  it("keeps known string functions as expressions and quotes unknown calls", () => {
    const rendered = renderModulePreview({
      instanceName: "worker",
      namespace: "platform",
      moduleName: "worker",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [
        { name: "slug", type: { kind: "string" }, value: { kind: "scalar", value: { mode: "literal", literal: 'replace(each.key, "_", "-")' } } },
        { name: "label", type: { kind: "string" }, value: { kind: "scalar", value: { mode: "literal", literal: "custom_helper(var.name)" } } },
        {
          name: "roles",
          type: { kind: "map", element: { kind: "object", attributes: [{ name: "name", type: { kind: "string" } }] } },
          value: {
            kind: "map",
            entries: [{
              key: "test",
              value: {
                kind: "object",
                entries: [{
                  name: "name",
                  value: { kind: "scalar", value: { mode: "literal", literal: 'lambda-execution-${replace(each.key, "_", "-")}' } },
                }],
              },
            }],
          },
        },
      ],
      referenceOptions,
      multiplicity: { kind: "none" },
      variableOptions,
      providerBindings: {},
      providerOptions,
    });

    expect(rendered).toContain('  slug  = replace(each.key, "_", "-")');
    expect(rendered).toContain('  label = "custom_helper(var.name)"');
    expect(rendered).toContain('name = "lambda-execution-${replace(each.key, "_", "-")}"');
  });

  it("includes repetition and provider bindings in the module call", () => {
    const rendered = renderModulePreview({
      instanceName: "worker",
      namespace: "platform",
      moduleName: "worker",
      system: "aws",
      version: "1.0.0",
      registryHost: "registry.example.com",
      inputs: [],
      referenceOptions,
      multiplicity: {
        kind: "count",
        mode: "conditional",
        expr: { mode: "reference", literal: "", refNodeId: "variable-count", refOutput: "" },
      },
      variableOptions,
      providerBindings: { aws: { providerNodeId: "provider-aws" } },
      providerOptions,
    });

    expect(rendered).toContain("count = var.enabled ? 1 : 0");
    expect(rendered).toContain("providers = {\n    aws = aws.primary\n  }");
  });
});