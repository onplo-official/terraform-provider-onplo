// Package provider is Onplo's OpenTofu provider: the resources and data sources of the table in
// table.go, each made from Onplo's public API contracts through the shared Go client.
//
// THE CREDENTIAL IS A SCOPED KEY FROM DEVELOPER ACCESS, given as api_key or in ONPLO_KEY, never a
// credential kind of its own: the provider acts with exactly the key's permissions, like the
// command line.
package provider

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"onplo.com/go-client/operation"
)

type onploProvider struct {
	version string
	kinds   []*kind
	fault   error // the table does not match the contracts
	http    *http.Client
}

var _ provider.Provider = (*onploProvider)(nil)

// New is the provider for a version, over the operations of the given domains.
func New(version string, domains []operation.Domain) func() provider.Provider {
	kinds, fault := resolve(table, domains)
	return func() provider.Provider {
		return &onploProvider{version: version, kinds: kinds, fault: fault, http: &http.Client{Timeout: 60 * time.Second}}
	}
}

func (p *onploProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "onplo"
	resp.Version = p.version
}

func (p *onploProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Onplo's own provider: your Onplo setup as code.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{Optional: true, Sensitive: true,
				Description: "A key from Developer Access. The provider acts with exactly its permissions. Left out, ONPLO_KEY is read."},
			"api_origin": schema.StringAttribute{Optional: true,
				Description: "Another origin to send the same calls to, such as staging. Plain http is refused except on this machine. Left out, ONPLO_API_ORIGIN is read, and then Onplo itself."},
		},
	}
	if p.fault != nil {
		resp.Diagnostics.AddError("The provider's table does not match Onplo's contracts", p.fault.Error())
	}
}

func (p *onploProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config struct {
		APIKey    types.String `tfsdk:"api_key"`
		APIOrigin types.String `tfsdk:"api_origin"`
	}
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.APIKey.IsUnknown() || config.APIOrigin.IsUnknown() {
		resp.Diagnostics.AddError("The provider's settings are not known yet",
			"api_key and api_origin must be known when the plan is made, not only after another resource is applied.")
		return
	}
	key := strings.TrimSpace(config.APIKey.ValueString())
	if key == "" {
		key = strings.TrimSpace(os.Getenv("ONPLO_KEY"))
	}
	if key == "" {
		resp.Diagnostics.AddError("No key to call Onplo with",
			"Set api_key, or ONPLO_KEY, to a key from Developer Access. The provider acts with exactly that key's permissions.")
		return
	}
	address := config.APIOrigin.ValueString()
	if address == "" {
		address = os.Getenv("ONPLO_API_ORIGIN")
	}
	c := &client{key: key, origin: address, http: p.http, version: p.version}
	resp.ResourceData, resp.DataSourceData = c, c
}

func (p *onploProvider) Resources(_ context.Context) []func() resource.Resource {
	var all []func() resource.Resource
	for _, k := range p.kinds {
		all = append(all, func() resource.Resource { return &resourceOf{k: k} })
	}
	return all
}

func (p *onploProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	var all []func() datasource.DataSource
	for _, k := range p.kinds {
		all = append(all, func() datasource.DataSource { return &dataSourceOf{k: k} })
	}
	return all
}

func clientFrom(data any, diags *diag.Diagnostics) *client {
	if data == nil {
		return nil // the provider is not configured yet: OpenTofu is only validating
	}
	c, ok := data.(*client)
	if !ok {
		diags.AddError("The provider handed over something unexpected", "This is a fault in the provider itself.")
	}
	return c
}
