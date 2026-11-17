package provider

import (
	"fmt"
	"regexp"
	"strings"

	"onplo.com/go-client/operation"
)

// kind is one row of the table, read against the contract: its operations and its attributes.
type kind struct {
	spec
	domain                    operation.Domain
	create, read, update, del *operation.Operation
	// listed is true when Read is a list (data and next_cursor) and the item is found in it.
	listed bool
	// identity is the path parameter that names one item: in Read, Update or Delete's path and not
	// in Create's. "" for a setting.
	identity string
	attrs    []attribute
}

func (k *kind) setting() bool { return k.Create == "" }

// attribute is one attribute of the resource and the field of the API it holds.
type attribute struct {
	name     string // in the file: the field's name, or <name>_value when that name is reserved
	field    operation.Field
	required bool // the file must give it
	optional bool // the file may give it; when it does not, Onplo's value is kept
	replace  bool // a change makes a new one, because the contract cannot change it in place
	inCreate bool // sent when it is made
	inUpdate bool // sent when it changes
	read     bool // Onplo's answer to Read carries it, in the same shape
	pathOf   bool // a path parameter of Create rather than a body field
	shown    bool // shown once, in Create's answer
}

func (a attribute) computed() bool { return !a.required }

// RESERVED NAMES. OpenTofu gives these words a meaning of its own in every resource block.
var reserved = map[string]bool{"connection": true, "count": true, "depends_on": true, "for_each": true,
	"lifecycle": true, "provider": true, "provisioner": true, "id": true}

var attributeName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// resolve reads every row of the table against the domains. A row naming an operation that is
// not in the contract is an error, so a rename in a contract is caught by the provider's tests.
func resolve(rows []spec, domains []operation.Domain) ([]*kind, error) {
	var kinds []*kind
	var faults []string
	seen := map[string]bool{}
	for _, row := range rows {
		k, err := resolveOne(row, domains)
		if err == nil && seen[row.Name] {
			err = fmt.Errorf("the name is in the table twice")
		}
		if err != nil {
			faults = append(faults, fmt.Sprintf("onplo_%s: %v", row.Name, err))
			continue
		}
		seen[row.Name] = true
		kinds = append(kinds, k)
	}
	if len(faults) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(faults, "; "))
	}
	return kinds, nil
}

func resolveOne(row spec, domains []operation.Domain) (*kind, error) {
	k := &kind{spec: row}
	found := false
	for _, d := range domains {
		if d.Name == row.Domain {
			k.domain, found = d, true
		}
	}
	if !found {
		return nil, fmt.Errorf("there is no outsiders' contract for %q", row.Domain)
	}
	var err error
	pick := func(id string) *operation.Operation {
		if id == "" || err != nil {
			return nil
		}
		for i := range k.domain.Operations {
			if k.domain.Operations[i].ID == id {
				return &k.domain.Operations[i]
			}
		}
		err = fmt.Errorf("the %s contract has no operation %q", row.Domain, id)
		return nil
	}
	k.create, k.read, k.update, k.del = pick(row.Create), pick(row.Read), pick(row.Update), pick(row.Delete)
	if err != nil {
		return nil, err
	}
	if k.read == nil || (k.setting() && k.update == nil) {
		return nil, fmt.Errorf("a resource needs a Read, and a setting an Update")
	}
	k.listed = isList(k.read)
	if !k.setting() {
		if k.identity, err = identityOf(k); err != nil {
			return nil, err
		}
	}
	if err := k.buildAttributes(); err != nil {
		return nil, err
	}
	return k, nil
}

// isList is true for a read whose answer is a page: data and next_cursor.
func isList(op *operation.Operation) bool {
	data, ok := fieldNamed(op.Answer, "data")
	_, cursor := fieldNamed(op.Answer, "next_cursor")
	return ok && cursor && data.Kind == operation.KindList && data.Element != nil &&
		data.Element.Kind == operation.KindObject
}

// identityOf is the one path parameter that names an item and that Create does not take.
func identityOf(k *kind) (string, error) {
	taken := map[string]bool{}
	for _, p := range pathParameters(k.create) {
		taken[p] = true
	}
	var names []string
	for _, op := range []*operation.Operation{k.read, k.update, k.del} {
		for _, p := range pathParameters(op) {
			if !taken[p] {
				names = append(names, p)
				taken[p] = true
			}
		}
	}
	if len(names) != 1 {
		return "", fmt.Errorf("want one path parameter naming the item, found %v", names)
	}
	return names[0], nil
}

func pathParameters(op *operation.Operation) []string {
	if op == nil {
		return nil
	}
	var names []string
	for _, p := range op.Parameters {
		if p.In == "path" {
			names = append(names, p.Name)
		}
	}
	return names
}

// readFields is what an answer to Read says about one item.
func (k *kind) readFields() []operation.Field {
	if k.listed {
		data, _ := fieldNamed(k.read.Answer, "data")
		return data.Element.Fields
	}
	return k.read.Answer
}

// buildAttributes makes the attributes, in this order: the path parameters Create takes, the
// fields it sends, the fields only an update sends, what Read answers, and what is shown once.
func (k *kind) buildAttributes() error {
	byName := map[string]int{}
	add := func(a attribute) *attribute {
		if i, known := byName[a.field.Name]; known {
			return &k.attrs[i]
		}
		byName[a.field.Name] = len(k.attrs)
		k.attrs = append(k.attrs, a)
		return &k.attrs[len(k.attrs)-1]
	}
	makes := k.create
	if k.setting() {
		makes = k.update
	}
	for _, p := range makes.Parameters {
		if p.In == "path" {
			add(attribute{field: operation.Field{Name: p.Name, Kind: operation.KindString, Required: true},
				required: true, replace: true, pathOf: true})
		}
	}
	updateFields := map[string]bool{}
	if k.update != nil {
		for _, f := range k.update.Body {
			updateFields[f.Name] = true
		}
	}
	for _, f := range makes.Body {
		a := add(attribute{field: f, inCreate: true})
		a.inUpdate = k.setting() || updateFields[f.Name]
		a.required = f.Required
		a.optional = !f.Required
		a.replace = !a.inUpdate
	}
	if k.update != nil && !k.setting() {
		for _, f := range k.update.Body {
			if _, known := byName[f.Name]; !known {
				add(attribute{field: f, optional: true, inUpdate: true})
			}
		}
	}
	for _, f := range k.readFields() {
		if f.Name == "id" {
			continue
		}
		a := add(attribute{field: f})
		a.read = sameShape(a.field, f)
		a.field.Sensitive = a.field.Sensitive || f.Sensitive
	}
	if len(k.ShownOnce) > 0 {
		for _, name := range k.ShownOnce {
			f, ok := fieldNamed(k.create.Answer, name)
			if !ok {
				return fmt.Errorf("%s's answer has no field %q to keep", k.Create, name)
			}
			f.Sensitive = true
			add(attribute{field: f, shown: true})
		}
	}
	for i := range k.attrs {
		name := k.attrs[i].field.Name
		if reserved[name] {
			name += "_value"
		}
		if !attributeName.MatchString(name) {
			return fmt.Errorf("the field %q cannot be an attribute name", k.attrs[i].field.Name)
		}
		k.attrs[i].name = name
	}
	return nil
}

// sameShape is true when two fields hold the same kind of value, all the way down.
func sameShape(a, b operation.Field) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case operation.KindList:
		return a.Element != nil && b.Element != nil && sameShape(*a.Element, *b.Element)
	case operation.KindObject:
		if len(a.Fields) != len(b.Fields) {
			return false
		}
		for i := range a.Fields {
			if a.Fields[i].Name != b.Fields[i].Name || !sameShape(a.Fields[i], b.Fields[i]) {
				return false
			}
		}
	}
	return true
}

func fieldNamed(fields []operation.Field, name string) (operation.Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return operation.Field{}, false
}
