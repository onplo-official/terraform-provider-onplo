package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"onplo.com/go-client/operation"
)

// THE VALUES ARE TRANSLATED ONCE, HERE, between what OpenTofu holds (tftypes) and what Onplo's
// API carries (JSON), and the field from the contract says how. Every resource uses these two.

// toJSON is a value from the file as JSON. ok is false for a null or a value not yet known, which
// is never sent: Onplo keeps what it has.
func toJSON(f operation.Field, v tftypes.Value) (out any, ok bool, err error) {
	if !v.IsKnown() || v.IsNull() {
		return nil, false, nil
	}
	switch f.Kind {
	case operation.KindString, operation.KindJSON:
		var s string
		if err := v.As(&s); err != nil {
			return nil, false, err
		}
		if f.Kind == operation.KindString {
			return s, true, nil
		}
		decoder := json.NewDecoder(bytes.NewReader([]byte(s)))
		decoder.UseNumber()
		if err := decoder.Decode(&out); err != nil {
			return nil, false, fmt.Errorf("%s is not JSON text: %v", f.Name, err)
		}
		return out, true, nil
	case operation.KindInteger, operation.KindNumber:
		var n big.Float
		if err := v.As(&n); err != nil {
			return nil, false, err
		}
		return json.Number(n.Text('g', -1)), true, nil
	case operation.KindBoolean:
		var b bool
		err := v.As(&b)
		return b, err == nil, err
	case operation.KindList:
		var items []tftypes.Value
		if err := v.As(&items); err != nil {
			return nil, false, err
		}
		list := make([]any, 0, len(items))
		for _, item := range items {
			value, _, err := toJSON(*f.Element, item)
			if err != nil {
				return nil, false, err
			}
			list = append(list, value)
		}
		return list, true, nil
	case operation.KindObject:
		var fields map[string]tftypes.Value
		if err := v.As(&fields); err != nil {
			return nil, false, err
		}
		object := map[string]any{}
		for _, child := range f.Fields {
			value, given, err := toJSON(child, fields[child.Name])
			if err != nil {
				return nil, false, err
			}
			if given {
				object[child.Name] = value
			}
		}
		return object, true, nil
	}
	return nil, false, fmt.Errorf("%s has a kind this provider does not know: %q", f.Name, f.Kind)
}

// fromJSON is a value from Onplo's answer, in the type OpenTofu holds it in. An answer that does
// not fit the contract is an error, never a guess.
func fromJSON(f operation.Field, value any, t tftypes.Type) (tftypes.Value, error) {
	if value == nil {
		return tftypes.NewValue(t, nil), nil
	}
	misfit := func() (tftypes.Value, error) {
		return tftypes.Value{}, fmt.Errorf("Onplo answered %s as %T, and the contract says it is %s", f.Name, value, f.Kind)
	}
	switch f.Kind {
	case operation.KindString:
		s, ok := value.(string)
		if !ok {
			return misfit()
		}
		return tftypes.NewValue(t, s), nil
	case operation.KindJSON:
		text, err := canonical(value)
		if err != nil {
			return tftypes.Value{}, err
		}
		return tftypes.NewValue(t, text), nil
	case operation.KindInteger, operation.KindNumber:
		number, ok := value.(json.Number)
		if !ok {
			return misfit()
		}
		n, _, err := big.ParseFloat(number.String(), 10, 512, big.ToNearestEven)
		if err != nil {
			return misfit()
		}
		return tftypes.NewValue(t, n), nil
	case operation.KindBoolean:
		b, ok := value.(bool)
		if !ok {
			return misfit()
		}
		return tftypes.NewValue(t, b), nil
	case operation.KindList:
		items, ok := value.([]any)
		if !ok {
			return misfit()
		}
		element := t.(tftypes.List).ElementType
		list := make([]tftypes.Value, 0, len(items))
		for _, item := range items {
			v, err := fromJSON(*f.Element, item, element)
			if err != nil {
				return tftypes.Value{}, err
			}
			list = append(list, v)
		}
		return tftypes.NewValue(t, list), nil
	case operation.KindObject:
		object, ok := value.(map[string]any)
		if !ok {
			return misfit()
		}
		types := t.(tftypes.Object).AttributeTypes
		fields := map[string]tftypes.Value{}
		for _, child := range f.Fields {
			v, err := fromJSON(child, object[child.Name], types[child.Name])
			if err != nil {
				return tftypes.Value{}, err
			}
			fields[child.Name] = v
		}
		return tftypes.NewValue(t, fields), nil
	}
	return misfit()
}

// canonical is JSON text written one way: keys sorted, no spaces, nothing escaped that need not
// be. jsonencode() in a file writes the same.
func canonical(value any) (string, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return string(bytes.TrimRight(b.Bytes(), "\n")), nil
}

// sameJSON is true when the file's value and Onplo's say the same thing, however each is written.
func sameJSON(f operation.Field, v tftypes.Value, answer any) bool {
	given, ok, err := toJSON(f, v)
	if !ok || err != nil {
		return false
	}
	a, errA := canonical(given)
	b, errB := canonical(answer)
	return errA == nil && errB == nil && a == b
}
