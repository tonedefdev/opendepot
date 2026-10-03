import { describe, expect, it } from "vitest";
import type { Multiplicity, TypeSpec, ValueSpec, VariableOption } from "./types";
import {
  emptyValueSpec,
  forEachShapeFromTypeSpec,
  isComplexVariableType,
  metaExprCtyType,
  objectAttributePaths,
  orderedObjectAttributes,
  renderTypeSpec,
  renderValueSpec,
  renderVariableSpec,
  typeSpecToCtyType,
} from "./typeSpec";

const nestedType: TypeSpec = {
  kind: "object",
  attributes: [
    {
      name: "services",
      type: {
        kind: "map",
        element: {
          kind: "object",
          attributes: [
            {
              name: "ports",
              type: { kind: "list", element: { kind: "number" } },
            },
          ],
        },
      },
    },
    {
      name: "labels",
      type: { kind: "map", element: { kind: "string" } },
      optional: true,
      hasDefault: true,
      default: {
        kind: "map",
        entries: [{ key: "managed-by", value: { kind: "scalar", literal: "opendepot" } }],
      },
    },
  ],
};

const nestedValue: ValueSpec = {
  kind: "object",
  entries: [
    {
      name: "services",
      value: {
        kind: "map",
        entries: [
          {
            key: "api",
            value: {
              kind: "object",
              entries: [
                {
                  name: "ports",
                  value: {
                    kind: "list",
                    items: [
                      { kind: "scalar", literal: "8080" },
                      { kind: "scalar", literal: "8081" },
                    ],
                  },
                },
              ],
            },
          },
        ],
      },
    },
  ],
};

describe("recursive type specifications", () => {
  it("identifies only object and map-object variables as complex", () => {
    expect(isComplexVariableType({ kind: "object", attributes: [] })).toBe(true);
    expect(isComplexVariableType({ kind: "map", element: { kind: "object", attributes: [] } })).toBe(true);
    expect(isComplexVariableType({ kind: "list", element: { kind: "string" } })).toBe(false);
    expect(isComplexVariableType({ kind: "bool" })).toBe(false);
  });
  it("orders object attributes by scalar fields before structure", () => {
    const attributes = orderedObjectAttributes([
      { name: "sid", type: { kind: "string" as const } },
      { name: "resources", type: { kind: "list" as const, element: { kind: "string" as const } } },
      { name: "effect", type: { kind: "string" as const } },
      { name: "actions", type: { kind: "list" as const, element: { kind: "string" as const } } },
      { name: "conditions", type: { kind: "list" as const, element: { kind: "string" as const } } },
      { name: "name", type: { kind: "string" as const } },
    ]);

    expect(attributes.map((attribute) => attribute.name)).toEqual([
      "effect",
      "name",
      "sid",
      "actions",
      "conditions",
      "resources",
    ]);
  });

  it("renders expression-backed map keys without string quotes", () => {
    const type: TypeSpec = { kind: "map", element: { kind: "string" } };

    expect(
      renderValueSpec(type, {
        kind: "map",
        entries: [
          { key: "each.key", value: { kind: "scalar", literal: "each.value" } },
          { key: "primary", value: { kind: "scalar", literal: "us-east-1" } },
        ],
      }),
    ).toBe(`{
  (each.key) = "each.value"
  "primary"  = "us-east-1"
}`);
  });

  it("preserves known function calls in string values", () => {
    const type: TypeSpec = { kind: "string" };

    expect(renderValueSpec(type, { kind: "scalar", literal: 'replace(each.key, "_", "-")' })).toBe('replace(each.key, "_", "-")');
    expect(renderValueSpec(type, { kind: "scalar", literal: "replace-this" })).toBe('"replace-this"');
  });

  it("renders deeply nested types and structured optional defaults", () => {
    const rendered = renderTypeSpec(nestedType);

    expect(rendered).toContain("services = map(object({");
    expect(rendered).toContain("ports = list(number)");
    expect(rendered).toContain('labels   = optional(map(string), {\n    "managed-by" = "opendepot"\n  })');
  });

  it("renders deeply nested values", () => {
    const rendered = renderValueSpec(nestedType, nestedValue);

    expect(rendered).toContain('"api" = {');
    expect(rendered).toContain("ports = [8080, 8081]");
  });

  it("formats variable blocks like tofu fmt", () => {
    const type: TypeSpec = {
      kind: "map",
      element: {
        kind: "object",
        attributes: [
          {
            name: "spec",
            type: {
              kind: "object",
              attributes: [
                {
                  name: "bucket_name",
                  type: { kind: "string" },
                  optional: true,
                  hasDefault: true,
                  default: { kind: "scalar", literal: "" },
                },
              ],
            },
          },
        ],
      },
    };

    expect(renderVariableSpec("create_kms_key", type, true, { kind: "map", entries: [] })).toBe(`variable "create_kms_key" {
  default = {}
  type = map(object({
    spec = object({
      bucket_name = optional(string, null)
    })
  }))
}`);
  });

  it("renders variable descriptions and ordered validations", () => {
    expect(
      renderVariableSpec(
        "region",
        { kind: "string" },
        false,
        { kind: "scalar", literal: "" },
        "Primary AWS region.",
        [
          { condition: "length(var.region) > 0", errorMessage: "Region is required." },
          { condition: 'var.region != "moon-1"', errorMessage: "Region must be supported." },
        ],
      ),
    ).toBe(`variable "region" {
  type = string
  description = "Primary AWS region."

  validation {
    condition     = length(var.region) > 0
    error_message = "Region is required."
  }

  validation {
    condition     = var.region != "moon-1"
    error_message = "Region must be supported."
  }
}`);
  });

  it("indents multiline validation conditions as HCL expressions", () => {
    const condition = "alltrue([\n  for _, function in var.lambda_functions :\n  length(function.spec.description) <= 100\n])";
    const rendered = renderVariableSpec("lambda_functions", { kind: "string" }, false, { kind: "scalar", literal: "" }, "", [
      { condition, errorMessage: "The description must be less than or equal to 100 characters." },
    ]);

    expect(rendered).toContain(`    condition     = alltrue([
      for _, function in var.lambda_functions :
      length(function.spec.description) <= 100
    ])`);
  });

  it("aligns sibling assignments within every nested scope", () => {
    const type: TypeSpec = {
      kind: "map",
      element: {
        kind: "object",
        attributes: [
          {
            name: "spec",
            type: {
              kind: "object",
              attributes: [
                {
                  name: "bucket_name",
                  type: { kind: "string" },
                  optional: true,
                  hasDefault: true,
                  default: { kind: "scalar", literal: "opendepot-example" },
                },
                {
                  name: "create_bucket",
                  type: { kind: "bool" },
                  optional: true,
                  hasDefault: true,
                  default: { kind: "scalar", literal: "false" },
                },
              ],
            },
          },
        ],
      },
    };
    const value: ValueSpec = {
      kind: "map",
      entries: [
        {
          key: "example",
          value: {
            kind: "object",
            entries: [
              {
                name: "spec",
                value: {
                  kind: "object",
                  entries: [
                    { name: "bucket_name", value: { kind: "scalar", literal: "assembly-line-bucket" } },
                    { name: "create_bucket", value: { kind: "scalar", literal: "true" } },
                  ],
                },
              },
            ],
          },
        },
      ],
    };

    expect(renderVariableSpec("s3_buckets", type, true, value)).toBe(`variable "s3_buckets" {
  default = {
    "example" = {
      spec = {
        bucket_name   = "assembly-line-bucket"
        create_bucket = true
      }
    }
  }
  type = map(object({
    spec = object({
      bucket_name   = optional(string, "opendepot-example")
      create_bucket = optional(bool, false)
    })
  }))
}`);
  });

  it("converts every nested level to CtyType", () => {
    expect(typeSpecToCtyType(nestedType)).toEqual([
      "object",
      {
        services: ["map", ["object", { ports: ["list", "number"] }]],
        labels: ["map", "string"],
      },
    ]);
  });

  it("enumerates every dot-addressable nested object path", () => {
    expect(objectAttributePaths(nestedType).map(({ path, type }) => [path, type.kind])).toEqual([
      ["services", "map"],
      ["labels", "map"],
    ]);

    const objectType: TypeSpec = {
      kind: "object",
      attributes: [
        {
          name: "spec",
          type: {
            kind: "object",
            attributes: [
              {
                name: "network",
                type: {
                  kind: "object",
                  attributes: [{ name: "cidr", type: { kind: "string" } }],
                },
              },
            ],
          },
        },
      ],
    };

    expect(objectAttributePaths(objectType).map(({ path, type }) => [path, type.kind])).toEqual([
      ["spec", "object"],
      ["spec.network", "object"],
      ["spec.network.cidr", "string"],
    ]);
  });

  it("enumerates and types nested each.value object paths", () => {
    const sourceType: TypeSpec = {
      kind: "map",
      element: {
        kind: "object",
        attributes: [
          {
            name: "spec",
            type: {
              kind: "object",
              attributes: [
                { name: "bucket_name", type: { kind: "string" } },
                { name: "create_bucket", type: { kind: "bool" } },
              ],
            },
          },
        ],
      },
    };
    const multiplicity: Multiplicity = { kind: "for_each", variableNodeId: "variable-1" };
    const variableOptions: VariableOption[] = [{ nodeId: "variable-1", name: "s3_objects", type: sourceType }];

    expect(forEachShapeFromTypeSpec(sourceType).attributes).toEqual([
      "spec",
      "spec.bucket_name",
      "spec.create_bucket",
    ]);
    expect(metaExprCtyType("each.value.spec.bucket_name", multiplicity, variableOptions)).toBe("string");
    expect(metaExprCtyType("each.value.spec.create_bucket", multiplicity, variableOptions)).toBe("bool");
  });

  it("creates structurally complete tuple defaults", () => {
    expect(emptyValueSpec({ kind: "tuple", elements: [{ kind: "string" }, { kind: "map", element: { kind: "bool" } }] })).toEqual({
      kind: "list",
      items: [
        { kind: "scalar", literal: "" },
        { kind: "map", entries: [] },
      ],
    });
  });
});
