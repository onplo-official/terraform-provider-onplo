package main

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"onplo.com/go-client/operation"
)

// HOW DEEP A BODY IS READ. Below this a value is carried as JSON text. Our contracts nest three or
// four levels at most; the limit is what stops a schema that refers to itself.
const deepest = 5

// bodyFields is the request body's fields, when the body is a JSON object.
func bodyFields(op *openapi3.Operation) []operation.Field {
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil
	}
	return objectFields(op.RequestBody.Value.Content.Get("application/json"))
}

// answerFields is the fields of the first success answer that carries a JSON body.
func answerFields(op *openapi3.Operation) []operation.Field {
	if op.Responses == nil {
		return nil
	}
	codes := make([]string, 0, op.Responses.Len())
	for code := range op.Responses.Map() {
		if strings.HasPrefix(code, "2") {
			codes = append(codes, code)
		}
	}
	sort.Strings(codes)
	for _, code := range codes {
		answer := op.Responses.Value(code)
		if answer == nil || answer.Value == nil {
			continue
		}
		if fields := objectFields(answer.Value.Content.Get("application/json")); fields != nil {
			return fields
		}
	}
	return nil
}

func objectFields(media *openapi3.MediaType) []operation.Field {
	if media == nil || media.Schema == nil {
		return nil
	}
	f := fieldOf("", media.Schema, false, 0, true)
	if f.Kind != operation.KindObject {
		return nil
	}
	return f.Fields
}

// fieldOf reads one schema as a Field. A single allOf is the contract's way of adding a
// description to a shared schema, so it is looked through; an anyOf with null is a value that may
// be null; any other union is JSON text.
func fieldOf(name string, ref *openapi3.SchemaRef, required bool, depth int, describe bool) operation.Field {
	f := operation.Field{Name: name, Kind: operation.KindJSON, Required: required}
	if ref == nil || ref.Value == nil {
		return f
	}
	s := ref.Value
	if describe {
		f.Description = firstParagraph(s.Description)
	}
	f.Sensitive = s.WriteOnly || s.Format == "password"
	if inner := lookThrough(s); inner != nil {
		g := fieldOf(name, inner, required, depth, describe)
		if f.Description != "" {
			g.Description = f.Description
		}
		g.Sensitive = g.Sensitive || f.Sensitive
		return g
	}
	switch kind := typeOf(s); kind {
	case "string":
		f.Kind = operation.KindString
	case "integer":
		f.Kind = operation.KindInteger
	case "number":
		f.Kind = operation.KindNumber
	case "boolean":
		f.Kind = operation.KindBoolean
	case "array":
		if depth < deepest {
			element := fieldOf("", s.Items, false, depth+1, false)
			f.Kind, f.Element = operation.KindList, &element
		}
	case "object":
		if len(s.Properties) > 0 && depth < deepest {
			f.Kind = operation.KindObject
			for _, property := range sortedKeys(s.Properties) {
				f.Fields = append(f.Fields, fieldOf(property, s.Properties[property],
					contains(s.Required, property), depth+1, depth == 0))
			}
		}
	}
	return f
}

// lookThrough is the one schema a wrapper stands for: a lone allOf, or an anyOf/oneOf of one
// schema and null. Nil when the schema is not such a wrapper.
func lookThrough(s *openapi3.Schema) *openapi3.SchemaRef {
	if len(s.Properties) > 0 {
		return nil
	}
	if len(s.AllOf) == 1 {
		return s.AllOf[0]
	}
	for _, union := range []openapi3.SchemaRefs{s.AnyOf, s.OneOf} {
		var kept []*openapi3.SchemaRef
		for _, member := range union {
			if member == nil || member.Value == nil || typeOf(member.Value) != "null" {
				kept = append(kept, member)
			}
		}
		if len(union) > 0 && len(kept) == 1 {
			return kept[0]
		}
	}
	return nil
}

// typeOf is the schema's one type other than null, or "" when it has none or several.
func typeOf(s *openapi3.Schema) string {
	if s.Type != nil {
		var types []string
		for _, t := range *s.Type {
			if t != "null" {
				types = append(types, t)
			}
		}
		if len(types) == 1 {
			return types[0]
		}
		if len(types) == 0 && len(*s.Type) > 0 {
			return "null"
		}
		return ""
	}
	if len(s.Properties) > 0 {
		return "object"
	}
	if len(s.Enum) > 0 && len(s.AnyOf)+len(s.OneOf)+len(s.AllOf) == 0 {
		for _, value := range s.Enum {
			if _, isString := value.(string); !isString {
				return ""
			}
		}
		return "string"
	}
	return ""
}

func firstParagraph(text string) string {
	paragraph, _, _ := strings.Cut(strings.TrimSpace(text), "\n\n")
	return strings.Join(strings.Fields(paragraph), " ")
}

func contains(list []string, item string) bool {
	for _, x := range list {
		if x == item {
			return true
		}
	}
	return false
}

// writeFields writes fields as a Go literal of []operation.Field.
func writeFields(b *bytes.Buffer, fields []operation.Field) {
	b.WriteString("[]operation.Field{")
	for _, f := range fields {
		writeField(b, f)
		b.WriteString(",")
	}
	b.WriteString("}")
}

func writeField(b *bytes.Buffer, f operation.Field) {
	fmt.Fprintf(b, "{Name: %q, Kind: %q, Required: %t, Sensitive: %t, Description: %q",
		f.Name, f.Kind, f.Required, f.Sensitive, f.Description)
	if f.Element != nil {
		b.WriteString(", Element: &operation.Field")
		writeField(b, *f.Element)
	}
	if len(f.Fields) > 0 {
		b.WriteString(", Fields: ")
		writeFields(b, f.Fields)
	}
	b.WriteString("}")
}
