package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"onplo.com/go-client/operation"
)

// typeOf is the type a field is held in. A value of more than one shape is JSON text, which a
// file writes with jsonencode().
func typeOf(f operation.Field) attr.Type {
	switch f.Kind {
	case operation.KindInteger:
		return types.Int64Type
	case operation.KindNumber:
		return types.Float64Type
	case operation.KindBoolean:
		return types.BoolType
	case operation.KindList:
		return types.ListType{ElemType: typeOf(*f.Element)}
	case operation.KindObject:
		fields := map[string]attr.Type{}
		for _, child := range f.Fields {
			fields[child.Name] = typeOf(child)
		}
		return types.ObjectType{AttrTypes: fields}
	default:
		return types.StringType
	}
}

func (a attribute) description() string {
	text := a.field.Description
	if a.field.Kind == operation.KindJSON {
		text += " Given as JSON text, with jsonencode()."
	}
	switch {
	case a.shown:
		text += " Shown only when it is made, and kept here since there is no other moment to read it."
	case a.replace && !a.computed():
		text += " Changing it makes a new one."
	case a.optional && !a.inCreate:
		text += " It can be set once it exists."
	}
	return text
}

// resourceSchema is the resource's schema: what the file gives, and what Onplo fills in.
func (k *kind) resourceSchema() rschema.Schema {
	idText := "Onplo's id for it."
	if k.setting() {
		idText = fmt.Sprintf("Always %q: there is one of this setting, and it always exists.", k.Name)
	}
	s := rschema.Schema{
		Description: k.summary(),
		Attributes: map[string]rschema.Attribute{
			"id": rschema.StringAttribute{Computed: true, Description: idText,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
	for _, a := range k.attrs {
		s.Attributes[a.name] = a.resourceAttribute()
	}
	return s
}

func (a attribute) resourceAttribute() rschema.Attribute {
	required, optional, computed := a.required, a.optional, a.computed()
	sensitive, text := a.field.Sensitive, a.description()
	switch a.field.Kind {
	case operation.KindInteger:
		return rschema.Int64Attribute{Required: required, Optional: optional, Computed: computed, Sensitive: sensitive,
			Description: text, PlanModifiers: replaceIf(a, int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace())}
	case operation.KindNumber:
		return rschema.Float64Attribute{Required: required, Optional: optional, Computed: computed, Sensitive: sensitive,
			Description: text, PlanModifiers: replaceIf(a, float64planmodifier.UseStateForUnknown(), float64planmodifier.RequiresReplace())}
	case operation.KindBoolean:
		return rschema.BoolAttribute{Required: required, Optional: optional, Computed: computed, Sensitive: sensitive,
			Description: text, PlanModifiers: replaceIf(a, boolplanmodifier.UseStateForUnknown(), boolplanmodifier.RequiresReplace())}
	case operation.KindList:
		return rschema.ListAttribute{ElementType: typeOf(*a.field.Element), Required: required, Optional: optional,
			Computed: computed, Sensitive: sensitive, Description: text,
			PlanModifiers: replaceIf(a, listplanmodifier.UseStateForUnknown(), listplanmodifier.RequiresReplace())}
	case operation.KindObject:
		return rschema.ObjectAttribute{AttributeTypes: typeOf(a.field).(types.ObjectType).AttrTypes, Required: required,
			Optional: optional, Computed: computed, Sensitive: sensitive, Description: text,
			PlanModifiers: replaceIf(a, objectplanmodifier.UseStateForUnknown(), objectplanmodifier.RequiresReplace())}
	default:
		return rschema.StringAttribute{Required: required, Optional: optional, Computed: computed, Sensitive: sensitive,
			Description: text, PlanModifiers: replaceIf(a, stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace())}
	}
}

// AN ATTRIBUTE THAT ONLY ONPLO WRITES IS UNKNOWN UNTIL APPLIED, never kept from the state with
// UseStateForUnknown: Onplo may change it on any write (a time, a count), and a planned value that
// then differs is an error in OpenTofu. The one exception is a field that only Create sends, which
// no later call can change: left out of the file, it keeps Onplo's value, or every change to any
// other attribute would read as a change to it and make a new one.
func replaceIf[M any](a attribute, keep, replace M) []M {
	switch {
	case !a.replace:
		return nil
	case a.computed():
		return []M{keep, replace}
	default:
		return []M{replace}
	}
}

// dataSourceSchema is the same attributes, all read: the id names which one, unless it is a setting.
func (k *kind) dataSourceSchema() dschema.Schema {
	id := dschema.StringAttribute{Required: true, Description: "Onplo's id for the one to read."}
	if k.setting() {
		id = dschema.StringAttribute{Computed: true, Description: fmt.Sprintf("Always %q.", k.Name)}
	}
	s := dschema.Schema{Description: k.summary(), Attributes: map[string]dschema.Attribute{"id": id}}
	// What Read never answers (a field only ever sent, a secret shown once) is left out: a data
	// source would hold it as null for ever.
	for _, a := range k.attrs {
		if a.read || a.pathOf {
			s.Attributes[a.name] = a.dataSourceAttribute(a.pathOf)
		}
	}
	return s
}

func (a attribute) dataSourceAttribute(given bool) dschema.Attribute {
	required, computed := given, !given
	sensitive, text := a.field.Sensitive, a.field.Description
	switch a.field.Kind {
	case operation.KindInteger:
		return dschema.Int64Attribute{Required: required, Computed: computed, Sensitive: sensitive, Description: text}
	case operation.KindNumber:
		return dschema.Float64Attribute{Required: required, Computed: computed, Sensitive: sensitive, Description: text}
	case operation.KindBoolean:
		return dschema.BoolAttribute{Required: required, Computed: computed, Sensitive: sensitive, Description: text}
	case operation.KindList:
		return dschema.ListAttribute{ElementType: typeOf(*a.field.Element), Required: required, Computed: computed,
			Sensitive: sensitive, Description: text}
	case operation.KindObject:
		return dschema.ObjectAttribute{AttributeTypes: typeOf(a.field).(types.ObjectType).AttrTypes, Required: required,
			Computed: computed, Sensitive: sensitive, Description: text}
	default:
		return dschema.StringAttribute{Required: required, Computed: computed, Sensitive: sensitive, Description: text}
	}
}

// summary is what the resource is, in the contract's own words.
func (k *kind) summary() string {
	from := k.create
	if from == nil {
		from = k.update
	}
	if from.Summary == "" {
		return fmt.Sprintf("onplo_%s, from %s's contract.", k.Name, k.Domain)
	}
	return fmt.Sprintf("%s (%s's contract, %s).", from.Summary, k.Domain, from.ID)
}
