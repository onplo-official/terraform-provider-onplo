package provider

// THESE TESTS SPEAK THE PLUGIN PROTOCOL ITSELF, the calls OpenTofu makes to a provider, against a
// stand-in Onplo, over the provider built from the real contracts, with no OpenTofu program, so
// they run everywhere (ONPLO-5161). acceptance_test.go lets OpenTofu itself send them, on the
// checks machine, against the same stand-in (ONPLO-5240).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"onplo.com/go-client/generated/operations"
)

// standIn is a stand-in Onplo holding agents and one retention setting.
type standIn struct {
	mu        sync.Mutex
	agents    map[string]map[string]any
	retention map[string]any
	calls     []string
	refuse    bool
}

func (s *standIn) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer k_test" {
		http.Error(w, `{"code":"not_signed_in"}`, http.StatusUnauthorized)
		return
	}
	if s.refuse {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"forbidden","message":"This key may not build agents."}`))
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	answer := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch id := strings.TrimPrefix(r.URL.Path, "/v1/agents/"); {
	case r.Method == "POST" && r.URL.Path == "/v1/agents":
		agent := map[string]any{"id": "agt_1", "state": "draft", "created_at": "2026-11-14T10:00:00Z",
			"username": strings.ToLower(body["name"].(string)), "memory_store_ids": []any{}}
		for k, v := range body {
			agent[k] = v
		}
		s.agents["agt_1"] = agent
		answer(201, agent)
	case r.URL.Path == "/v1/retention" && r.Method == "PUT":
		s.retention = map[string]any{"version_history_days": body["version_history_days"],
			"trash_days": body["trash_days"], "longest_version_history_days": 365, "longest_trash_days": 90}
		answer(200, s.retention)
	case r.URL.Path == "/v1/retention":
		answer(200, s.retention)
	case s.agents[id] == nil:
		answer(404, map[string]any{"code": "not_found"})
	case r.Method == "GET":
		answer(200, s.agents[id])
	case r.Method == "PATCH":
		for k, v := range body {
			s.agents[id][k] = v
		}
		answer(200, s.agents[id])
	case r.Method == "DELETE":
		delete(s.agents, id)
		w.WriteHeader(http.StatusNoContent)
	}
}

// harness is one provider, configured against the stand-in, and the schemas it gave.
type harness struct {
	t       *testing.T
	ctx     context.Context
	server  tfprotov6.ProviderServer
	schemas *tfprotov6.GetProviderSchemaResponse
	onplo   *standIn
}

func start(t *testing.T) *harness {
	t.Helper()
	onplo := &standIn{agents: map[string]map[string]any{}}
	stand := httptest.NewServer(onplo)
	t.Cleanup(stand.Close)
	server, err := providerserver.NewProtocol6WithError(New("test", operations.All)())()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, ctx: context.Background(), server: server, onplo: onplo}
	h.schemas, err = server.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	h.clean("the provider's schema", h.schemas.Diagnostics)
	providerType := h.schemas.Provider.ValueType().(tftypes.Object)
	config := h.object(providerType, map[string]tftypes.Value{
		"api_key":    tftypes.NewValue(tftypes.String, "k_test"),
		"api_origin": tftypes.NewValue(tftypes.String, stand.URL),
	})
	configured, err := server.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{Config: h.dynamic(config)})
	if err != nil {
		t.Fatal(err)
	}
	h.clean("configuring the provider", configured.Diagnostics)
	return h
}

func (h *harness) clean(what string, diags []*tfprotov6.Diagnostic) {
	h.t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			h.t.Fatalf("%s: %s: %s", what, d.Summary, d.Detail)
		}
	}
}

func (h *harness) object(t tftypes.Object, set map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{}
	for name, attributeType := range t.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
		if v, given := set[name]; given {
			values[name] = v
		}
	}
	return tftypes.NewValue(t, values)
}

func (h *harness) dynamic(v tftypes.Value) *tfprotov6.DynamicValue {
	h.t.Helper()
	d, err := tfprotov6.NewDynamicValue(v.Type(), v)
	if err != nil {
		h.t.Fatal(err)
	}
	return &d
}

func (h *harness) typeOf(resource string) tftypes.Object {
	h.t.Helper()
	s, ok := h.schemas.ResourceSchemas[resource]
	if !ok {
		h.t.Fatalf("there is no resource %s", resource)
	}
	return s.ValueType().(tftypes.Object)
}

// apply plans and applies a file's values against the prior state, as OpenTofu does, and gives
// the new state and what the provider said. A null config removes it.
func (h *harness) apply(resource string, prior tftypes.Value, set map[string]tftypes.Value) (tftypes.Value, []*tfprotov6.Diagnostic) {
	h.t.Helper()
	t := h.typeOf(resource)
	config := tftypes.NewValue(t, nil)
	planned := h.dynamic(config)
	var private []byte
	if set != nil {
		config = h.object(t, set)
		proposed := h.proposed(resource, config, prior)
		plan, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{TypeName: resource,
			PriorState: h.dynamic(prior), ProposedNewState: h.dynamic(proposed), Config: h.dynamic(config)})
		if err != nil {
			h.t.Fatal(err)
		}
		h.clean("planning "+resource, plan.Diagnostics)
		planned, private = plan.PlannedState, plan.PlannedPrivate
	}
	applied, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{TypeName: resource,
		PriorState: h.dynamic(prior), PlannedState: planned, Config: h.dynamic(config), PlannedPrivate: private})
	if err != nil {
		h.t.Fatal(err)
	}
	state := tftypes.NewValue(t, nil)
	if applied.NewState != nil {
		if state, err = applied.NewState.Unmarshal(t); err != nil {
			h.t.Fatal(err)
		}
	}
	return state, applied.Diagnostics
}

// proposed is the file's values, with what only Onplo writes carried from the prior state, the
// way OpenTofu proposes a new state before the provider plans it.
func (h *harness) proposed(resource string, config, prior tftypes.Value) tftypes.Value {
	if prior.IsNull() {
		return config
	}
	var given, before map[string]tftypes.Value
	_ = config.As(&given)
	_ = prior.As(&before)
	for _, a := range h.schemas.ResourceSchemas[resource].Block.Attributes {
		if a.Computed && given[a.Name].IsNull() {
			given[a.Name] = before[a.Name]
		}
	}
	return tftypes.NewValue(config.Type(), given)
}

func (h *harness) read(resource string, state tftypes.Value) tftypes.Value {
	h.t.Helper()
	read, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{TypeName: resource, CurrentState: h.dynamic(state)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.clean("reading "+resource, read.Diagnostics)
	out, err := read.NewState.Unmarshal(h.typeOf(resource))
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func text(v tftypes.Value, name string) string {
	var values map[string]tftypes.Value
	_ = v.As(&values)
	return stringOf(values[name])
}

func str(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }

var agentFile = map[string]tftypes.Value{
	"name": str("Helper"), "instructions": str("Be kind."), "owner_scope": str("company"),
}

// The normal case: an agent is made, read, changed in place and removed, each with one call.
func TestAnAgentIsMadeChangedAndRemoved(t *testing.T) {
	h := start(t)
	none := tftypes.NewValue(h.typeOf("onplo_ai_agent"), nil)
	made, diags := h.apply("onplo_ai_agent", none, agentFile)
	h.clean("making the agent", diags)
	if text(made, "id") != "agt_1" || text(made, "username") != "helper" || text(made, "state") != "draft" {
		t.Fatalf("the state is not Onplo's answer: %v", made)
	}
	if text(h.read("onplo_ai_agent", made), "name") != "Helper" {
		t.Fatal("reading it back lost the name")
	}
	renamed := map[string]tftypes.Value{}
	for k, v := range agentFile {
		renamed[k] = v
	}
	renamed["name"] = str("Helper two")
	changed, diags := h.apply("onplo_ai_agent", made, renamed)
	h.clean("renaming the agent", diags)
	if text(changed, "name") != "Helper two" || text(changed, "id") != "agt_1" {
		t.Fatalf("the rename did not happen in place: %v", changed)
	}
	_, diags = h.apply("onplo_ai_agent", changed, nil)
	h.clean("removing the agent", diags)
	if len(h.onplo.agents) != 0 {
		t.Fatal("the agent is still in Onplo")
	}
	want := "POST /v1/agents,GET /v1/agents/agt_1,GET /v1/agents/agt_1,PATCH /v1/agents/agt_1,GET /v1/agents/agt_1,DELETE /v1/agents/agt_1"
	if got := strings.Join(h.onplo.calls, ","); got != want {
		t.Fatalf("calls\n got %s\nwant %s", got, want)
	}
}

// The main failure: Onplo refuses, and the person sees Onplo's own words.
func TestARefusalIsShownInOnplosWords(t *testing.T) {
	h := start(t)
	h.onplo.refuse = true
	_, diags := h.apply("onplo_ai_agent", tftypes.NewValue(h.typeOf("onplo_ai_agent"), nil), agentFile)
	if len(diags) == 0 || !strings.Contains(diags[0].Detail, "403") || !strings.Contains(diags[0].Detail, "may not build agents") {
		t.Fatalf("want Onplo's refusal, got %v", diags)
	}
}

// Removed in Onplo by someone else: the next read drops it from the state, so the plan makes it again.
func TestAnAgentRemovedInOnploLeavesTheState(t *testing.T) {
	h := start(t)
	made, diags := h.apply("onplo_ai_agent", tftypes.NewValue(h.typeOf("onplo_ai_agent"), nil), agentFile)
	h.clean("making the agent", diags)
	delete(h.onplo.agents, "agt_1")
	if !h.read("onplo_ai_agent", made).IsNull() {
		t.Fatal("an agent Onplo no longer has stayed in the state")
	}
}

// A setting is set with one call, and leaving it out of the file leaves it as it is in Onplo.
func TestASettingIsSetAndKeptWhenLeftOut(t *testing.T) {
	h := start(t)
	set, diags := h.apply("onplo_storage_retention", tftypes.NewValue(h.typeOf("onplo_storage_retention"), nil),
		map[string]tftypes.Value{"version_history_days": tftypes.NewValue(tftypes.Number, 30),
			"trash_days": tftypes.NewValue(tftypes.Number, 14)})
	h.clean("setting retention", diags)
	if text(set, "id") != "storage_retention" {
		t.Fatalf("a setting's id is its name, got %q", text(set, "id"))
	}
	before := len(h.onplo.calls)
	_, diags = h.apply("onplo_storage_retention", set, nil)
	if len(h.onplo.calls) != before || len(diags) != 1 || diags[0].Severity != tfprotov6.DiagnosticSeverityWarning {
		t.Fatalf("leaving a setting out called Onplo or did not say so: %v", diags)
	}
}

// Every row of the table names operations that exist, and a secret shown once is sensitive.
func TestEveryRowOfTheTableMatchesTheContracts(t *testing.T) {
	h := start(t)
	if len(h.schemas.ResourceSchemas) != len(table) || len(h.schemas.DataSourceSchemas) != len(table) {
		t.Fatalf("%d resources and %d data sources for %d rows", len(h.schemas.ResourceSchemas),
			len(h.schemas.DataSourceSchemas), len(table))
	}
	for _, a := range h.schemas.ResourceSchemas["onplo_integrations_access_key"].Block.Attributes {
		if a.Name == "secret" && (!a.Sensitive || !a.Computed) {
			t.Fatal("the access key's secret is not kept as a sensitive value Onplo writes")
		}
	}
}
