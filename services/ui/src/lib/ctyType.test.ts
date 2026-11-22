import { describe, expect, it } from "vitest";

import {
  CONFIDENCE_COLOR,
  GRADE_COLOR,
  INSTANCE_NAME_PATTERN,
  isValidInstanceName,
  literalValidationError,
  renderTypeCompact,
  typesRoughlyCompatible,
} from "./ctyType";

describe("color maps", () => {
  it("maps compatibility grades to badge colors", () => {
    expect(GRADE_COLOR).toEqual({ full: "success", partial: "warning", unsupported: "default" });
  });

  it("maps output confidence to badge colors", () => {
    expect(CONFIDENCE_COLOR).toEqual({ exact: "success", inferred: "info", unknown: "default" });
  });
});

describe("renderTypeCompact", () => {
  it("returns primitive names unchanged", () => {
    expect(renderTypeCompact("string")).toBe("string");
    expect(renderTypeCompact("bool")).toBe("bool");
    expect(renderTypeCompact("dynamic")).toBe("dynamic");
  });

  it("renders collection types with their element type", () => {
    expect(renderTypeCompact(["list", "string"])).toBe("list(string)");
    expect(renderTypeCompact(["set", ["map", "number"]])).toBe("set(map(number))");
    expect(renderTypeCompact(["map", ["list", "bool"]])).toBe("map(list(bool))");
  });

  it("summarizes objects and tuples by their attribute or element count", () => {
    expect(renderTypeCompact(["object", { name: "string", size: "number", tags: ["list", "string"], enabled: "bool" }])).toBe("object(4)");
    expect(renderTypeCompact(["object", {}])).toBe("object(0)");
    expect(renderTypeCompact(["tuple", ["string", "number"]])).toBe("tuple(2)");
    expect(renderTypeCompact(["tuple", []])).toBe("tuple(0)");
  });

  it("falls back when the object or tuple payload is missing", () => {
    expect(renderTypeCompact(["object"])).toBe("object(0)");
    expect(renderTypeCompact(["tuple"])).toBe("tuple(0)");
    expect(renderTypeCompact(["list"])).toBe("list(dynamic)");
  });

  it("renders an empty or non-array encoding as dynamic", () => {
    expect(renderTypeCompact([])).toBe("dynamic");
  });

  it("returns the kind name for any other tuple head", () => {
    expect(renderTypeCompact(["capsule", "string"])).toBe("capsule");
  });
});

describe("typesRoughlyCompatible", () => {
  it("treats dynamic on either side as compatible", () => {
    expect(typesRoughlyCompatible("dynamic", ["list", "string"])).toBe(true);
    expect(typesRoughlyCompatible(["object", {}], "dynamic")).toBe(true);
    expect(typesRoughlyCompatible([], "bool")).toBe(true);
  });

  it("accepts matching top-level kinds regardless of element types", () => {
    expect(typesRoughlyCompatible("bool", "bool")).toBe(true);
    expect(typesRoughlyCompatible(["list", "string"], ["list", "number"])).toBe(true);
    expect(typesRoughlyCompatible(["map", "string"], ["map", "bool"])).toBe(true);
  });

  it("treats string and number as interchangeable", () => {
    expect(typesRoughlyCompatible("string", "number")).toBe(true);
    expect(typesRoughlyCompatible("number", "string")).toBe(true);
  });

  it("flags clearly different top-level kinds", () => {
    expect(typesRoughlyCompatible("bool", "string")).toBe(false);
    expect(typesRoughlyCompatible(["list", "string"], ["map", "string"])).toBe(false);
    expect(typesRoughlyCompatible(["object", {}], ["tuple", []])).toBe(false);
    expect(typesRoughlyCompatible("string", ["list", "string"])).toBe(false);
  });
});

describe("isValidInstanceName", () => {
  it("accepts HCL identifiers with letters, digits, underscores, and hyphens", () => {
    expect(isValidInstanceName("keypair")).toBe(true);
    expect(isValidInstanceName("_private")).toBe(true);
    expect(isValidInstanceName("vpc-main_01")).toBe(true);
  });

  it("rejects names that start with a digit or hyphen, or contain other characters", () => {
    expect(isValidInstanceName("")).toBe(false);
    expect(isValidInstanceName("1vpc")).toBe(false);
    expect(isValidInstanceName("-vpc")).toBe(false);
    expect(isValidInstanceName("vpc.main")).toBe(false);
    expect(isValidInstanceName("vpc main")).toBe(false);
  });

  it("exposes the same pattern it validates against", () => {
    expect(INSTANCE_NAME_PATTERN.test("valid_name")).toBe(true);
    expect(INSTANCE_NAME_PATTERN.test("invalid name")).toBe(false);
  });
});

describe("literalValidationError", () => {
  it("does not flag empty or whitespace-only literals", () => {
    expect(literalValidationError("number", "")).toBeNull();
    expect(literalValidationError("bool", "   ")).toBeNull();
  });

  it("accepts each and count self-references regardless of the variable type", () => {
    expect(literalValidationError("number", "each.key")).toBeNull();
    expect(literalValidationError("bool", "each.value")).toBeNull();
    expect(literalValidationError("number", "each.value.subnet_id")).toBeNull();
    expect(literalValidationError("number", "count.index")).toBeNull();
  });

  it("requires numeric literals for number types", () => {
    expect(literalValidationError("number", "42")).toBeNull();
    expect(literalValidationError("number", " 3.5 ")).toBeNull();
    expect(literalValidationError("number", "abc")).toBe("Must be a number");
    expect(literalValidationError(["number"], "not-a-number")).toBe("Must be a number");
    expect(literalValidationError("number", "each.key-suffix")).toBe("Must be a number");
  });

  it("requires true or false for bool types, ignoring case", () => {
    expect(literalValidationError("bool", "true")).toBeNull();
    expect(literalValidationError("bool", "FALSE")).toBeNull();
    expect(literalValidationError("bool", "yes")).toBe("Must be true or false");
  });

  it("accepts any literal for string, collection, and dynamic types", () => {
    expect(literalValidationError("string", "anything at all")).toBeNull();
    expect(literalValidationError(["list", "string"], "[1, 2]")).toBeNull();
    expect(literalValidationError(["map", "bool"], "{ a = true }")).toBeNull();
    expect(literalValidationError("dynamic", "abc")).toBeNull();
    expect(literalValidationError([], "abc")).toBeNull();
  });
});
