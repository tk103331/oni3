package custom

import (
	"testing"

	"github.com/tk103331/oni3/config"
)

func TestCustomToolParameterNamesAndTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		defs []config.ToolParameter
	}{
		{"invalid name", []config.ToolParameter{{Name: "cityName", Type: "string"}}},
		{"invalid type", []config.ToolParameter{{Name: "city", Type: "String"}}},
		{"duplicate name", []config.ToolParameter{{Name: "city", Type: "string"}, {Name: "city", Type: "string"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := toolParameters(tc.defs); err == nil {
				t.Fatal("accepted ambiguous parameter definition")
			}
		})
	}
	cfg := config.Tool{Parameters: []config.ToolParameter{{Name: "city_name", Type: "string", Required: true}}}
	params, err := toolParameters(cfg.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := params.ToJSONSchema()
	if err != nil {
		t.Fatal(err)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "city_name" {
		t.Fatalf("required parameter lost: %+v", schema)
	}
}
