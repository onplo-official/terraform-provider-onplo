package main

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"

	"github.com/getkin/kin-openapi/openapi3"

	"onplo.com/go-client/operation"
)

// operationsFile writes every document's operations as one Go table, operations.All: each one's
// method, path and parameters, and the fields of its JSON body and of its answer.
func operationsFile(docs []document) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(banner)
	b.WriteString("// Package operations is every operation of Onplo's public API, as data.\n")
	b.WriteString("package operations\n\nimport \"onplo.com/go-client/operation\"\n\n")
	b.WriteString("// All is every outsiders' domain, in the order of their folders under api-contracts/.\n")
	b.WriteString("var All = []operation.Domain{\n")
	for _, d := range docs {
		domain, err := domainOf(d)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.File, err)
		}
		writeDomain(&b, domain)
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

func writeDomain(b *bytes.Buffer, d operation.Domain) {
	fmt.Fprintf(b, "{Name: %q, Server: %q, Operations: []operation.Operation{\n", d.Name, d.Server)
	for _, op := range d.Operations {
		fmt.Fprintf(b, "{ID: %q, Method: %q, Path: %q, Summary: %q, HasBody: %t, SignedOut: %t, Parameters: []operation.Parameter{",
			op.ID, op.Method, op.Path, op.Summary, op.HasBody, op.SignedOut)
		for _, p := range op.Parameters {
			fmt.Fprintf(b, "{Name: %q, In: %q, Required: %t},", p.Name, p.In, p.Required)
		}
		b.WriteString("}")
		if op.Body != nil {
			b.WriteString(", Body: ")
			writeFields(b, op.Body)
		}
		if op.Answer != nil {
			b.WriteString(", Answer: ")
			writeFields(b, op.Answer)
		}
		b.WriteString("},\n")
	}
	b.WriteString("}},\n")
}

// domainOf reads one document's operations, sorted by their id so the table never reorders.
func domainOf(d document) (operation.Domain, error) {
	domain := operation.Domain{Name: d.Domain, Server: d.Server}
	if d.Spec.Paths == nil {
		return domain, nil
	}
	seen := map[string]string{}
	for path, item := range d.Spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" {
				return domain, fmt.Errorf("%s %s has no operationId, so no command can be named after it", method, path)
			}
			if other, taken := seen[op.OperationID]; taken {
				return domain, fmt.Errorf("operationId %q is used twice: %s and %s %s", op.OperationID, other, method, path)
			}
			seen[op.OperationID] = method + " " + path
			domain.Operations = append(domain.Operations, operation.Operation{
				ID:         op.OperationID,
				Method:     method,
				Path:       path,
				Summary:    op.Summary,
				Parameters: parametersOf(item.Parameters, op.Parameters),
				HasBody:    op.RequestBody != nil,
				SignedOut:  signedOut(d.Spec, op),
				Body:       bodyFields(op),
				Answer:     answerFields(op),
			})
		}
	}
	sort.Slice(domain.Operations, func(i, j int) bool { return domain.Operations[i].ID < domain.Operations[j].ID })
	return domain, nil
}

// parametersOf merges a path's own parameters with an operation's, the operation's winning, as
// OpenAPI says. Cookie parameters are left out: no Onplo contract reads one from an outsider.
func parametersOf(shared, own openapi3.Parameters) []operation.Parameter {
	byKey := map[string]operation.Parameter{}
	var order []string
	for _, list := range []openapi3.Parameters{shared, own} {
		for _, ref := range list {
			if ref == nil || ref.Value == nil || ref.Value.In == openapi3.ParameterInCookie {
				continue
			}
			key := ref.Value.In + ":" + ref.Value.Name
			if _, known := byKey[key]; !known {
				order = append(order, key)
			}
			byKey[key] = operation.Parameter{Name: ref.Value.Name, In: ref.Value.In, Required: ref.Value.Required}
		}
	}
	params := make([]operation.Parameter, 0, len(order))
	for _, key := range order {
		params = append(params, byKey[key])
	}
	return params
}

// signedOut is true when the operation needs no credential: its own `security: []`, or no
// security anywhere in the document.
func signedOut(spec *openapi3.T, op *openapi3.Operation) bool {
	if op.Security != nil {
		return len(*op.Security) == 0
	}
	return len(spec.Security) == 0
}
