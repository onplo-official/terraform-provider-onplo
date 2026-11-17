package provider

// THESE TESTS RUN OPENTOFU ITSELF against the provider and a stand-in Onplo (ONPLO-5240). The
// protocol tests in provider_test.go send the calls OpenTofu would send; these let OpenTofu send
// them, so a fault only its own planning shows is caught too: a plan that is not empty straight
// after an apply, a value the provider changed between plan and apply, an import that does not
// read back what was made.
//
// THEY RUN ONLY WITH TF_ACC=1, the testing library's own switch. The Go job on the checks machine
// sets it, with TF_ACC_TERRAFORM_PATH naming the OpenTofu pinned in pinned-toolchains.tsv, so the
// library never fetches a program of its own. Without it they are skipped, and the protocol tests
// still run.

import (
	"fmt"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"onplo.com/go-client/generated/operations"
)

// underOpenTofu is the stand-in Onplo, and the provider as OpenTofu starts it.
func underOpenTofu(t *testing.T) (*standIn, string, map[string]func() (tfprotov6.ProviderServer, error)) {
	t.Helper()
	// OpenTofu reads an unqualified provider from its own registry. The testing library hands the
	// provider over at registry.terraform.io unless told otherwise, and OpenTofu would then look
	// for a provider nobody published. So the address is OpenTofu's.
	t.Setenv("TF_ACC_PROVIDER_HOST", "registry.opentofu.org")
	onplo := &standIn{agents: map[string]map[string]any{}}
	stand := httptest.NewServer(onplo)
	t.Cleanup(stand.Close)
	factories := map[string]func() (tfprotov6.ProviderServer, error){
		"onplo": providerserver.NewProtocol6WithError(New("test", operations.All)()),
	}
	return onplo, stand.URL, factories
}

func agentConfig(origin, name string) string {
	return fmt.Sprintf(`
provider "onplo" {
  api_key    = "k_test"
  api_origin = %q
}

resource "onplo_ai_agent" "helper" {
  name         = %q
  instructions = "Be kind."
  owner_scope  = "company"
}
`, origin, name)
}

// The normal case, through OpenTofu: an agent is made, renamed in place, imported by its id, and
// removed. After every apply the library plans again and fails on anything but an empty plan.
func TestAccAnAgentThroughOpenTofu(t *testing.T) {
	onplo, origin, factories := underOpenTofu(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: agentConfig(origin, "Helper"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("onplo_ai_agent.helper", "id", "agt_1"),
					resource.TestCheckResourceAttr("onplo_ai_agent.helper", "username", "helper"),
					resource.TestCheckResourceAttr("onplo_ai_agent.helper", "state", "draft"),
				),
			},
			{
				Config: agentConfig(origin, "Helper two"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("onplo_ai_agent.helper", "id", "agt_1"),
					resource.TestCheckResourceAttr("onplo_ai_agent.helper", "name", "Helper two"),
				),
			},
			{
				Config:            agentConfig(origin, "Helper two"),
				ResourceName:      "onplo_ai_agent.helper",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
		CheckDestroy: func(*terraform.State) error {
			onplo.mu.Lock()
			defer onplo.mu.Unlock()
			if len(onplo.agents) != 0 {
				return fmt.Errorf("OpenTofu destroyed the agent, and Onplo still has %d", len(onplo.agents))
			}
			return nil
		},
	})
}

// The main failure, through OpenTofu: Onplo refuses, and the person reads Onplo's own words.
func TestAccARefusalReachesThePersonThroughOpenTofu(t *testing.T) {
	onplo, origin, factories := underOpenTofu(t)
	onplo.refuse = true
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      agentConfig(origin, "Helper"),
			ExpectError: regexp.MustCompile(`may not build\s+agents`), // OpenTofu wraps the message at its width
		}},
	})
}

// A setting, through OpenTofu: set with one call, read back with nothing left to change.
func TestAccASettingThroughOpenTofu(t *testing.T) {
	_, origin, factories := underOpenTofu(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: fmt.Sprintf(`
provider "onplo" {
  api_key    = "k_test"
  api_origin = %q
}

resource "onplo_storage_retention" "this" {
  version_history_days = 30
  trash_days           = 14
}
`, origin),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("onplo_storage_retention.this", "id", "storage_retention"),
				resource.TestCheckResourceAttr("onplo_storage_retention.this", "version_history_days", "30"),
			),
		}},
	})
}
