package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// nameInlineTypes gives every inline schema that oapi-codegen would name like another type a name
// of its own, on the loaded document only — the contract on disk is never touched. It does so by
// lifting that schema into the document's components under the free name and pointing the
// property at it, because oapi-codegen reads a component's name plainly and ignores a naming
// extension on an inline union (the second proof run of ONPLO-5160).
//
// WHY. oapi-codegen names an inline property's type after its parent and the property:
// PurchaseAsk.answer becomes PurchaseAskAnswer. Our contracts often have a component of exactly
// that name (PurchaseAskAnswer is the request that answers one), so the two collide and oapi-codegen
// stops ("can't auto-rename"). On 14 November 2026 about sixty such pairs stood in nine contracts.
// Asking every contract to carry a Go-only annotation would put this program's problem in nine
// other homes, so the rule lives here, once: a taken name gains "Value" (PurchaseAskAnswerValue),
// and a number after it if that is taken too.
func nameInlineTypes(spec *openapi3.T) {
	if spec.Components == nil {
		return
	}
	taken := map[string]bool{}
	for name := range spec.Components.Schemas {
		taken[typeName(name)] = true
	}
	for _, name := range sortedKeys(spec.Components.Schemas) {
		nameInlineProperties(spec.Components.Schemas, spec.Components.Schemas[name], typeName(name), taken, 0)
	}
}

func nameInlineProperties(components openapi3.Schemas, ref *openapi3.SchemaRef, parent string, taken map[string]bool, depth int) {
	if ref == nil || ref.Value == nil || depth > 12 {
		return
	}
	for _, property := range sortedKeys(ref.Value.Properties) {
		child := ref.Value.Properties[property]
		if child == nil || child.Ref != "" || !makesAType(child.Value) {
			continue
		}
		name := parent + typeName(property)
		if taken[name] {
			name = freeName(name+"Value", taken)
			components[name] = &openapi3.SchemaRef{Value: child.Value}
			ref.Value.Properties[property] = openapi3.NewSchemaRef("#/components/schemas/"+name, child.Value)
		}
		taken[name] = true
		nameInlineProperties(components, child, name, taken, depth+1)
	}
	if items := ref.Value.Items; items != nil && items.Ref == "" {
		nameInlineProperties(components, items, parent+"Item", taken, depth+1)
	}
}

// makesAType is true for the inline schemas oapi-codegen writes a type of their own for.
func makesAType(s *openapi3.Schema) bool {
	return s != nil && (len(s.Enum) > 0 || len(s.Properties) > 0 || len(s.AnyOf) > 0 ||
		len(s.OneOf) > 0 || len(s.AllOf) > 0 || (s.Type != nil && s.Type.Is("object")))
}

func freeName(name string, taken map[string]bool) string {
	if !taken[name] {
		return name
	}
	for n := 2; ; n++ {
		if candidate := fmt.Sprintf("%s%d", name, n); !taken[candidate] {
			return candidate
		}
	}
}

var wordBreak = regexp.MustCompile(`[^A-Za-z0-9]+`)

// typeName is a schema or property name as oapi-codegen spells a Go type: memory_store_ids -> MemoryStoreIds.
func typeName(name string) string {
	var b strings.Builder
	for _, word := range wordBreak.Split(name, -1) {
		if word != "" {
			b.WriteString(strings.ToUpper(word[:1]) + word[1:])
		}
	}
	return b.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
