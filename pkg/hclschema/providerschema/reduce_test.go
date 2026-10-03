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

package providerschema

import (
	"testing"
)

func TestReduceRetainsProviderConfiguration(t *testing.T) {
	raw := []byte(`{
  "provider_schemas": {
    "registry.opentofu.org/hashicorp/aws": {
      "provider": {
        "block": {
          "attributes": {
            "region": {"type": "string", "optional": true},
            "access_key": {"type": "string", "required": true}
          },
          "block_types": {
            "assume_role": {
              "nesting_mode": "list",
			  "min_items": 1,
			  "max_items": 2,
              "block": {
                "attributes": {
                  "role_arn": {"type": "string", "required": true}
                }
              }
            }
          }
        }
      }
    }
  }
}`)

	schema, err := Reduce(raw, "hashicorp/aws", "5.0.0")
	if err != nil {
		t.Fatalf("Reduce() error = %v", err)
	}

	if schema.SchemaVersion != SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", schema.SchemaVersion, SchemaVersion)
	}

	region := schema.ProviderBlock.Attributes["region"]
	if !region.Optional || region.Required {
		t.Fatalf("region flags = required:%t optional:%t", region.Required, region.Optional)
	}

	accessKey := schema.ProviderBlock.Attributes["access_key"]
	if !accessKey.Required || accessKey.Optional {
		t.Fatalf("access_key flags = required:%t optional:%t", accessKey.Required, accessKey.Optional)
	}

	assumeRole := schema.ProviderBlock.Blocks["assume_role"]
	if assumeRole.Nesting != "list" || assumeRole.MinItems != 1 || assumeRole.MaxItems != 2 || !assumeRole.Block.Attributes["role_arn"].Required {
		t.Fatalf("assume_role = %#v", assumeRole)
	}
}
