package provider

import (
	"context"
	"fmt"
	"net/url"

	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// HOW EVERY RESOURCE TALKS TO ONPLO. Nothing here is about one resource: each step reads the
// kind's operations and attributes, which the table and the contract decided.

func (k *kind) attr(name string) (attribute, bool) {
	for _, a := range k.attrs {
		if a.name == name {
			return a, true
		}
	}
	return attribute{}, false
}

func objectOf(v tftypes.Value) (map[string]tftypes.Value, tftypes.Object, error) {
	t, ok := v.Type().(tftypes.Object)
	if !ok {
		return nil, t, fmt.Errorf("the value is not an object but %s", v.Type())
	}
	values := map[string]tftypes.Value{}
	if v.IsNull() || !v.IsKnown() {
		return values, t, nil
	}
	err := v.As(&values)
	return values, t, err
}

func stringOf(v tftypes.Value) string {
	var s string
	if v.IsKnown() && !v.IsNull() {
		_ = v.As(&s)
	}
	return s
}

// pathValues is the path parameters the file gives (a folder a project is made in), and the id.
func (k *kind) pathValues(given map[string]tftypes.Value, id string) map[string]string {
	path := map[string]string{}
	for _, a := range k.attrs {
		if a.pathOf {
			path[a.field.Name] = stringOf(given[a.name])
		}
	}
	if k.identity != "" && id != "" {
		path[k.identity] = id
	}
	return path
}

// body is the JSON body of the attributes wanted, from the file's values.
func (k *kind) body(given map[string]tftypes.Value, wanted func(attribute) bool) (map[string]any, error) {
	body := map[string]any{}
	for _, a := range k.attrs {
		if a.pathOf || !wanted(a) {
			continue
		}
		value, ok, err := toJSON(a.field, given[a.name])
		if err != nil {
			return nil, err
		}
		if ok {
			body[a.field.Name] = value
		}
	}
	return body, nil
}

// fetch reads one item. A list is read page by page until the item is found; not found is gone.
func (k *kind) fetch(ctx context.Context, c *client, id string, given map[string]tftypes.Value) (map[string]any, error) {
	path := k.pathValues(given, id)
	if !k.listed {
		return c.call(ctx, k.domain, k.read, path, nil, nil)
	}
	cursor := ""
	for page := 0; page < 1000; page++ {
		query := url.Values{}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		answer, err := c.call(ctx, k.domain, k.read, path, query, nil)
		if err != nil {
			return nil, err
		}
		items, _ := answer["data"].([]any)
		for _, item := range items {
			if object, ok := item.(map[string]any); ok && object["id"] == id {
				return object, nil
			}
		}
		next, _ := answer["next_cursor"].(string)
		if next == "" {
			break
		}
		cursor = next
	}
	return nil, &refusal{status: 404, text: fmt.Sprintf("%s does not list %q", k.read.ID, id)}
}

// idOf is the id in a create's answer: its own, or that of the one object it carries (a sign-in
// app's answer is the app and its secret, side by side).
func idOf(answer map[string]any) (string, error) {
	if id, ok := answer["id"].(string); ok && id != "" {
		return id, nil
	}
	var ids []string
	for _, value := range answer {
		if object, ok := value.(map[string]any); ok {
			if id, ok := object["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("Onplo's answer names no single id for what it made")
	}
	return ids[0], nil
}

// state is what OpenTofu keeps after a call: Onplo's answer for every attribute it carries, and
// the file's (or the last state's) value for the rest. When the file's value and Onplo's say the
// same thing, written differently, the file's way of writing it is kept.
func (k *kind) state(t tftypes.Object, id string, answer map[string]any, prior map[string]tftypes.Value) (tftypes.Value, error) {
	values := map[string]tftypes.Value{}
	for name, attributeType := range t.AttributeTypes {
		if name == "id" {
			values[name] = tftypes.NewValue(attributeType, id)
			continue
		}
		a, _ := k.attr(name)
		before, known := prior[name]
		known = known && before.IsFullyKnown()
		if value, carried := answer[a.field.Name]; a.read && carried {
			if known && !before.IsNull() && sameJSON(a.field, before, value) {
				values[name] = before
				continue
			}
			v, err := fromJSON(a.field, value, attributeType)
			if err != nil {
				return tftypes.Value{}, err
			}
			values[name] = v
			continue
		}
		if known {
			values[name] = before
		} else {
			values[name] = tftypes.NewValue(attributeType, nil)
		}
	}
	return tftypes.NewValue(t, values), nil
}
