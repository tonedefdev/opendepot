package providerschema

import (
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func lookupFixture() *ReducedSchema {
	return &ReducedSchema{
		Resources: map[string]Block{
			"example_thing": {
				Attributes: map[string]Attribute{
					"name":    {Type: []byte(`"string"`), Required: true},
					"tags":    {Type: []byte(`["map","string"]`), Optional: true},
					"config":  {Type: []byte(`["object",{"nested":["object",{"value":"number"}]}]`), Optional: true},
					"broken":  {Type: []byte(`["nope"]`), Computed: true},
					"untyped": {},
				},
				Blocks: map[string]NestedBlock{
					"rule": {
						Nesting: "list",
						Block: Block{
							Attributes: map[string]Attribute{
								"action": {Type: []byte(`"string"`), Required: true},
							},
						},
					},
					"ports": {
						Nesting: "set",
						Block: Block{
							Attributes: map[string]Attribute{
								"port": {Type: []byte(`"number"`)},
							},
						},
					},
					"labels": {
						Nesting: "map",
						Block: Block{
							Attributes: map[string]Attribute{
								"value": {Type: []byte(`"string"`)},
							},
						},
					},
					"single": {
						Nesting: "single",
						Block: Block{
							Attributes: map[string]Attribute{
								"enabled": {Type: []byte(`"bool"`)},
							},
						},
					},
				},
			},
		},
		DataSources: map[string]Block{
			"example_lookup": {
				Attributes: map[string]Attribute{
					"id": {Type: []byte(`"string"`), Computed: true},
				},
			},
		},
	}
}

func TestLookupResolvesAttributeTypes(t *testing.T) {
	schema := lookupFixture()

	tests := map[string]cty.Type{
		"name":                cty.String,
		"tags.env":            cty.String,
		"config.nested.value": cty.Number,
		"rule.action":         cty.String,
		"ports.port":          cty.Number,
		"labels.value":        cty.String,
		"single.enabled":      cty.Bool,
	}

	for path, want := range tests {
		got, ok := schema.Lookup("example_thing", path, false)
		if !ok {
			t.Errorf("Lookup(%q) not found", path)
			continue
		}

		if !got.Equals(want) {
			t.Errorf("Lookup(%q) = %#v, want %#v", path, got, want)
		}
	}
}

func TestLookupReturnsNestedBlockTypes(t *testing.T) {
	schema := lookupFixture()

	rule, ok := schema.Lookup("example_thing", "rule", false)
	if !ok || !rule.IsListType() || !rule.ElementType().HasAttribute("action") {
		t.Fatalf("Lookup(rule) = %#v, %t; want list of objects", rule, ok)
	}

	ports, ok := schema.Lookup("example_thing", "ports", false)
	if !ok || !ports.IsSetType() {
		t.Fatalf("Lookup(ports) = %#v, %t; want set", ports, ok)
	}

	labels, ok := schema.Lookup("example_thing", "labels", false)
	if !ok || !labels.IsMapType() {
		t.Fatalf("Lookup(labels) = %#v, %t; want map", labels, ok)
	}

	single, ok := schema.Lookup("example_thing", "single", false)
	if !ok || !single.IsObjectType() {
		t.Fatalf("Lookup(single) = %#v, %t; want object", single, ok)
	}
}

func TestLookupMissingAndInvalidPaths(t *testing.T) {
	schema := lookupFixture()

	misses := []struct {
		name, resource, path string
		isData               bool
	}{
		{name: "unknown resource", resource: "missing_thing", path: "name"},
		{name: "unknown attribute", resource: "example_thing", path: "absent"},
		{name: "unknown nested attribute", resource: "example_thing", path: "rule.absent"},
		{name: "walk past scalar", resource: "example_thing", path: "name.extra"},
		{name: "walk missing object attribute", resource: "example_thing", path: "config.absent"},
		{name: "undecodable attribute", resource: "example_thing", path: "broken"},
		{name: "undecodable attribute walk", resource: "example_thing", path: "broken.x"},
		{name: "data source requested as resource", resource: "example_lookup", path: "id"},
		{name: "resource requested as data source", resource: "example_thing", path: "name", isData: true},
	}

	for _, tt := range misses {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := schema.Lookup(tt.resource, tt.path, tt.isData)
			if ok {
				t.Fatalf("Lookup(%q, %q) = %#v, true; want not found", tt.resource, tt.path, got)
			}
		})
	}
}

func TestLookupResolvesDataSources(t *testing.T) {
	schema := lookupFixture()

	got, ok := schema.Lookup("example_lookup", "id", true)
	if !ok || !got.Equals(cty.String) {
		t.Fatalf("Lookup(data id) = %#v, %t; want string", got, ok)
	}
}

func TestResourceAttributesReturnsObjectType(t *testing.T) {
	schema := lookupFixture()

	got, ok := schema.ResourceAttributes("example_thing", false)
	if !ok || !got.IsObjectType() {
		t.Fatalf("ResourceAttributes = %#v, %t; want object", got, ok)
	}

	if !got.HasAttribute("name") || !got.AttributeType("name").Equals(cty.String) {
		t.Fatalf("name attribute missing from %#v", got)
	}

	if !got.AttributeType("untyped").Equals(cty.DynamicPseudoType) {
		t.Fatalf("untyped attribute = %#v, want dynamic", got.AttributeType("untyped"))
	}

	if !got.AttributeType("broken").Equals(cty.DynamicPseudoType) {
		t.Fatalf("broken attribute = %#v, want dynamic", got.AttributeType("broken"))
	}

	if !got.HasAttribute("rule") || !got.AttributeType("rule").IsListType() {
		t.Fatalf("rule block not wrapped as list: %#v", got.AttributeType("rule"))
	}

	data, ok := schema.ResourceAttributes("example_lookup", true)
	if !ok || !data.HasAttribute("id") {
		t.Fatalf("ResourceAttributes(data) = %#v, %t", data, ok)
	}

	if _, ok := schema.ResourceAttributes("missing", false); ok {
		t.Fatal("ResourceAttributes(missing) = found, want not found")
	}
}

func TestWrapNestingModes(t *testing.T) {
	inner := cty.Object(map[string]cty.Type{"v": cty.String})

	if !wrapNesting("list", inner).IsListType() {
		t.Error("list nesting did not produce a list")
	}

	if !wrapNesting("set", inner).IsSetType() {
		t.Error("set nesting did not produce a set")
	}

	if !wrapNesting("map", inner).IsMapType() {
		t.Error("map nesting did not produce a map")
	}

	if !wrapNesting("group", inner).Equals(inner) {
		t.Error("unknown nesting mode should return the inner type")
	}
}

func TestWalkIntoTypeHandlesMapsAndObjects(t *testing.T) {
	obj := cty.Object(map[string]cty.Type{
		"inner": cty.Map(cty.Number),
	})

	got, ok := walkIntoType(obj, []string{"inner", "anything"})
	if !ok || !got.Equals(cty.Number) {
		t.Fatalf("walkIntoType = %#v, %t; want number", got, ok)
	}

	if _, ok := walkIntoType(cty.String, []string{"x"}); ok {
		t.Fatal("walkIntoType into a primitive should fail")
	}
}

func TestDecodeTypeEmptyAndInvalid(t *testing.T) {
	got, err := decodeType(nil)
	if err != nil || !got.Equals(cty.DynamicPseudoType) {
		t.Fatalf("decodeType(nil) = %#v, %v; want dynamic", got, err)
	}

	if _, err := decodeType([]byte(`["nope"]`)); err == nil {
		t.Fatal("decodeType accepted an invalid cty type")
	}
}
