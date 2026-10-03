import { describe, expect, it } from "vitest";
import { assemblyResourceMatchesQuery, duplicateMapKeys, migrateStoredAssemblyNodes, serializeAssemblyDocument } from "./AssemblyCanvas";

const moduleNode = {
  id: "module-1",
  type: "module",
  position: { x: 10, y: 20 },
  data: {
    kind: "module",
    namespace: "platform",
    name: "vpc",
    version: "v1.2.3",
    instanceName: "network",
    grade: "full",
    variables: [],
    outputs: [],
    values: {},
    multiplicity: { kind: "none" },
    loading: false,
    error: null,
  },
};

describe("Assembly canvas persistence", () => {
  it("finds duplicate map keys, including nested maps", () => {
    expect(duplicateMapKeys({
      kind: "map",
      entries: [
        { key: "primary", value: { kind: "scalar", value: { mode: "literal", literal: "" } } },
        { key: " primary ", value: { kind: "scalar", value: { mode: "literal", literal: "" } } },
        {
          key: "secondary",
          value: {
            kind: "object",
            entries: [{
              name: "settings",
              value: {
                kind: "map",
                entries: [
                  { key: "region", value: { kind: "scalar", value: { mode: "literal", literal: "" } } },
                  { key: "region", value: { kind: "scalar", value: { mode: "literal", literal: "" } } },
                ],
              },
            }],
          },
        },
      ],
    })).toEqual(["primary", "region"]);
  });

  it("migrates v1 module snapshots with provider state", () => {
    const migrated = migrateStoredAssemblyNodes([moduleNode] as never);

    expect(migrated[0].data).toMatchObject({ requiredProviders: [], providerBindings: {} });
  });

  it("migrates legacy flat module values into scalar input values", () => {
    const migrated = migrateStoredAssemblyNodes([{
      ...moduleNode,
      data: {
        ...moduleNode.data,
        values: { region: { mode: "literal", literal: "us-east-1" } },
      },
    }] as never);

    expect((migrated[0].data as unknown as typeof moduleNode.data).values).toEqual({
      region: { kind: "scalar", value: { mode: "literal", literal: "us-east-1" } },
    });
  });

  it("keeps module snapshots with malformed structured entries loadable", () => {
    const migrated = migrateStoredAssemblyNodes([{
      ...moduleNode,
      data: {
        ...moduleNode.data,
        values: { settings: { kind: "map", entries: [null] } },
      },
    }] as never);

    expect(migrated).toHaveLength(1);
    expect((migrated[0].data as unknown as typeof moduleNode.data).values).toEqual({
      settings: { kind: "map", entries: [] },
    });
  });

  it("migrates stored variables with description and validation defaults", () => {
    const variableNode = {
      id: "variable-1",
      type: "variable",
      position: { x: 30, y: 40 },
      data: {
        kind: "variable",
        name: "region",
        type: { kind: "string" },
        hasDefault: false,
        default: { kind: "scalar", literal: "" },
      },
    };

    const migrated = migrateStoredAssemblyNodes([variableNode] as never);

    expect(migrated[0].data).toMatchObject({ description: "", validations: [] });
  });

  it("serializes variable descriptions and ordered validations", () => {
    const variableNode = {
      id: "variable-1",
      type: "variable",
      position: { x: 30, y: 40 },
      data: {
        kind: "variable",
        name: "region",
        type: { kind: "string" },
        description: "Primary AWS region.",
        validations: [
          { condition: "length(var.region) > 0", errorMessage: "Region is required." },
          { condition: 'var.region != "moon-1"', errorMessage: "Region must be supported." },
        ],
        hasDefault: false,
        default: { kind: "scalar", literal: "" },
      },
    };

    const document = serializeAssemblyDocument([variableNode] as never);

    expect(document.variables[0]).toMatchObject({
      description: "Primary AWS region.",
      validations: [
        { condition: "length(var.region) > 0", errorMessage: "Region is required." },
        { condition: 'var.region != "moon-1"', errorMessage: "Region must be supported." },
      ],
    });
  });

  it("omits blank module input literals while preserving configured inputs", () => {
    const configuredModule = {
      ...moduleNode,
      data: {
        ...moduleNode.data,
        values: {
          optional_name: { mode: "literal", literal: "   " },
          enabled: { mode: "literal", literal: "true" },
          subnet_id: { mode: "reference", literal: "", refNodeId: "module-2", refOutput: "subnet_id" },
        },
      },
    };

    const document = serializeAssemblyDocument([configuredModule] as never);

    expect(document.modules[0].values).toEqual({
      enabled: { mode: "literal", literal: "true" },
      subnet_id: { mode: "reference", literal: "", refNodeId: "module-2", refOutput: "subnet_id" },
    });
  });

  it("serializes provider bindings and recursive provider configuration", () => {
    const providerNode = {
      id: "provider-2",
      type: "provider",
      position: { x: 40, y: 50 },
      data: {
        kind: "provider",
        namespace: "platform",
        name: "aws",
        version: "v6.0.0",
        providerNamespace: "hashicorp",
        providerName: "aws",
        localName: "aws",
        alias: "primary",
        schema: {},
        configuration: {
          arguments: { region: { mode: "literal", literal: '"us-east-1"' } },
          blocks: { assume_role: [{ arguments: { role_arn: { mode: "literal", literal: '"arn:test"' } }, blocks: {} }] },
        },
        loading: false,
        error: null,
      },
    };
    const boundModule = {
      ...moduleNode,
      data: {
        ...moduleNode.data,
        requiredProviders: [{ localName: "aws", source: "hashicorp/aws" }],
        providerBindings: { aws: { providerNodeId: "provider-2" } },
      },
    };

    const document = serializeAssemblyDocument([boundModule, providerNode] as never);

    expect(document.schemaVersion).toBe("assembly.export.v1");
    expect(document.modules[0].providerBindings).toEqual({ aws: { providerNodeId: "provider-2" } });
    expect(document.providers[0]).toMatchObject({
      localName: "aws",
      alias: "primary",
      configuration: { arguments: { region: { literal: '"us-east-1"' } } },
    });
  });
});

describe("Assembly resource search", () => {
  it("ignores missing resource metadata while matching longer queries", () => {
    const resource = {
      name: "network",
      namespace: "platform",
      provider: undefined,
      providerNamespace: undefined,
    };

    expect(assemblyResourceMatchesQuery(resource, "workload")).toBe(false);
    expect(assemblyResourceMatchesQuery(resource, "network")).toBe(true);
  });
});
