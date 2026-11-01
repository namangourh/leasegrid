package lease

import (
	"reflect"
	"strings"
	"testing"
)

// TestJSONAndYAMLTagsMatch checks that every field has the same name in JSON
// and YAML output, and that a field JSON leaves out when zero is left out of
// YAML too. Without it, a new field given only a json tag would silently get
// a different name in YAML.
func TestJSONAndYAMLTagsMatch(t *testing.T) {
	for _, typ := range []reflect.Type{
		reflect.TypeFor[Lease](),
		reflect.TypeFor[Cost](),
		reflect.TypeFor[Resources](),
	} {
		for i := range typ.NumField() {
			f := typ.Field(i)
			jsonName, jsonOpts, _ := strings.Cut(f.Tag.Get("json"), ",")
			yamlName, yamlOpts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if jsonName == "" || jsonName != yamlName {
				t.Errorf("%s.%s: json name %q, yaml name %q, want the same non-empty name",
					typ.Name(), f.Name, jsonName, yamlName)
			}
			// JSON's omitzero and YAML's omitempty both leave out zero values.
			if (jsonOpts == "omitzero") != (yamlOpts == "omitempty") {
				t.Errorf("%s.%s: json options %q, yaml options %q, want both to omit zero values or neither",
					typ.Name(), f.Name, jsonOpts, yamlOpts)
			}
		}
	}
}
