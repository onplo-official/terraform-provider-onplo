package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// resourceOf is one row of the table as an OpenTofu resource.
type resourceOf struct {
	k *kind
	c *client
}

var (
	_ resource.ResourceWithConfigure   = (*resourceOf)(nil)
	_ resource.ResourceWithImportState = (*resourceOf)(nil)
)

func (r *resourceOf) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.k.Name
}

func (r *resourceOf) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = r.k.resourceSchema()
}

func (r *resourceOf) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (r *resourceOf) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	k := r.k
	plan, t, err := objectOf(req.Plan.Raw)
	if fail(&resp.Diagnostics, "Reading the plan", err) {
		return
	}
	makes := k.create
	if k.setting() {
		makes = k.update
	}
	body, err := k.body(plan, func(a attribute) bool { return a.inCreate })
	if fail(&resp.Diagnostics, "Reading the file's values", err) {
		return
	}
	answer, err := r.c.call(ctx, k.domain, makes, k.pathValues(plan, ""), nil, bodyFor(makes.HasBody, body))
	if fail(&resp.Diagnostics, fmt.Sprintf("Making onplo_%s", k.Name), err) {
		return
	}
	id := k.Name
	if !k.setting() {
		if id, err = idOf(answer); fail(&resp.Diagnostics, fmt.Sprintf("Making onplo_%s", k.Name), err) {
			return
		}
	}
	prior := map[string]tftypes.Value{}
	for name, v := range plan {
		prior[name] = v
	}
	for _, a := range k.attrs {
		if a.shown {
			v, err := fromJSON(a.field, answer[a.field.Name], t.AttributeTypes[a.name])
			if fail(&resp.Diagnostics, "Keeping what is shown once", err) {
				return
			}
			prior[a.name] = v
		}
	}
	// Saved before anything else can fail, so a later failure never leaves a made thing unknown.
	if saved, err := k.state(t, id, map[string]any{}, prior); err == nil {
		resp.State.Raw = saved
	}
	if !k.setting() && k.update != nil {
		later, err := k.body(plan, func(a attribute) bool { return a.inUpdate && !a.inCreate })
		if fail(&resp.Diagnostics, "Reading the file's values", err) {
			return
		}
		if len(later) > 0 {
			_, err := r.c.call(ctx, k.domain, k.update, k.pathValues(plan, id), nil, later)
			if fail(&resp.Diagnostics, fmt.Sprintf("Setting onplo_%s's fields that come after it is made", k.Name), err) {
				return
			}
		}
	}
	r.readInto(ctx, t, id, prior, &resp.State.Raw, &resp.Diagnostics)
}

func (r *resourceOf) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	current, t, err := objectOf(req.State.Raw)
	if fail(&resp.Diagnostics, "Reading the state", err) {
		return
	}
	id := stringOf(current["id"])
	item, err := r.k.fetch(ctx, r.c, id, current)
	if isGone(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if fail(&resp.Diagnostics, fmt.Sprintf("Reading onplo_%s %q", r.k.Name, id), err) {
		return
	}
	state, err := r.k.state(t, id, item, current)
	if fail(&resp.Diagnostics, fmt.Sprintf("Reading onplo_%s %q", r.k.Name, id), err) {
		return
	}
	resp.State.Raw = state
}

func (r *resourceOf) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	k := r.k
	plan, t, err := objectOf(req.Plan.Raw)
	if fail(&resp.Diagnostics, "Reading the plan", err) {
		return
	}
	current, _, err := objectOf(req.State.Raw)
	if fail(&resp.Diagnostics, "Reading the state", err) {
		return
	}
	id := stringOf(current["id"])
	body, err := k.body(plan, func(a attribute) bool { return a.inUpdate })
	if fail(&resp.Diagnostics, "Reading the file's values", err) {
		return
	}
	_, err = r.c.call(ctx, k.domain, k.update, k.pathValues(plan, id), nil, bodyFor(k.update.HasBody, body))
	if fail(&resp.Diagnostics, fmt.Sprintf("Changing onplo_%s %q", k.Name, id), err) {
		return
	}
	prior := map[string]tftypes.Value{}
	for name, v := range plan {
		prior[name] = v
	}
	for _, a := range k.attrs {
		if a.shown {
			prior[a.name] = current[a.name]
		}
	}
	r.readInto(ctx, t, id, prior, &resp.State.Raw, &resp.Diagnostics)
}

func (r *resourceOf) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	k := r.k
	if k.setting() {
		resp.Diagnostics.AddWarning(fmt.Sprintf("onplo_%s is no longer managed here", k.Name),
			"A setting always exists, so Onplo keeps it as it is now. Nothing was changed in Onplo.")
		return
	}
	current, _, err := objectOf(req.State.Raw)
	if fail(&resp.Diagnostics, "Reading the state", err) {
		return
	}
	id := stringOf(current["id"])
	_, err = r.c.call(ctx, k.domain, k.del, k.pathValues(current, id), nil, nil)
	if err != nil && !isGone(err) {
		fail(&resp.Diagnostics, fmt.Sprintf("Removing onplo_%s %q", k.Name, id), err)
	}
}

// ImportState takes Onplo's id for one that already exists; a setting takes any id.
func (r *resourceOf) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	if r.k.setting() {
		id = r.k.Name
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// readInto reads the item after a write, so the state is what Onplo holds and not what was sent.
func (r *resourceOf) readInto(ctx context.Context, t tftypes.Object, id string, prior map[string]tftypes.Value,
	into *tftypes.Value, diags *diag.Diagnostics) {
	item, err := r.k.fetch(ctx, r.c, id, prior)
	if fail(diags, fmt.Sprintf("Reading onplo_%s %q after writing it", r.k.Name, id), err) {
		return
	}
	state, err := r.k.state(t, id, item, prior)
	if fail(diags, fmt.Sprintf("Reading onplo_%s %q after writing it", r.k.Name, id), err) {
		return
	}
	*into = state
}

// dataSourceOf is one row of the table as an OpenTofu data source: the same thing, only read.
type dataSourceOf struct {
	k *kind
	c *client
}

var _ datasource.DataSourceWithConfigure = (*dataSourceOf)(nil)

func (d *dataSourceOf) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + d.k.Name
}

func (d *dataSourceOf) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = d.k.dataSourceSchema()
}

func (d *dataSourceOf) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.c = clientFrom(req.ProviderData, &resp.Diagnostics)
}

func (d *dataSourceOf) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	given, t, err := objectOf(req.Config.Raw)
	if fail(&resp.Diagnostics, "Reading the file's values", err) {
		return
	}
	id := d.k.Name
	if !d.k.setting() {
		id = stringOf(given["id"])
	}
	item, err := d.k.fetch(ctx, d.c, id, given)
	if fail(&resp.Diagnostics, fmt.Sprintf("Reading onplo_%s %q", d.k.Name, id), err) {
		return
	}
	state, err := d.k.state(t, id, item, given)
	if fail(&resp.Diagnostics, fmt.Sprintf("Reading onplo_%s %q", d.k.Name, id), err) {
		return
	}
	resp.State.Raw = state
}

// bodyFor is the body to send: none when the operation takes none.
func bodyFor(takesOne bool, body map[string]any) map[string]any {
	if !takesOne {
		return nil
	}
	return body
}

func fail(diags *diag.Diagnostics, what string, err error) bool {
	if err == nil {
		return false
	}
	diags.AddError(what, err.Error())
	return true
}
