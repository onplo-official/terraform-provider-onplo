# terraform-provider-onplo — Onplo's OpenTofu provider

**It holds Onplo's own provider: a customer writes their Onplo setup in a file, and OpenTofu makes
Onplo match it.** It runs in Terraform and OpenTofu alike, since both speak the same plugin
protocol. It is written in Go (architecture decision record 038), on the Go client it shares with
the command line, and it is published as `onplo-official/terraform-provider-onplo`, the name both
registries require (architecture decision record 039). The developer-tools lane owns it.

```hcl
provider "onplo" {}                     # the key comes from ONPLO_KEY

resource "onplo_ai_agent" "helper" {
  name         = "Helper"
  instructions = "Answer questions about our handbook."
  owner_scope  = "company"
}

resource "onplo_storage_retention" "this" {
  version_history_days = 90
  trash_days           = 30
}
```

## How it works

* **Every resource is a few operation ids, and nothing more.** `internal/provider/table.go` names,
  for each resource, the operations that make it, read it, change it and remove it. Its
  attributes come from the contract, through the shared client's operations table: a field
  added to a contract is an attribute the day it lands.
* **The table is the one judgement here: what is configuration.** A team, an agent, a key ring or
  a retention period is something kept in a file. A file, a chat or a job is content, and is not
  in the table.
* **What the contract says decides the attribute.**
  * A field Create requires is required, and any other field it takes is optional.
  * A field that only Create takes cannot change in place, so changing it makes a new one.
  * A field only the answer carries is filled in by Onplo.
  * A value of more than one shape is given as JSON text, with `jsonencode()`.
* **A setting** (retention, rules, defaults) always exists, one per environment or per person.
  Applying it sets it. Removing it from the file leaves it as it is in Onplo, and the provider says
  so.
* **A secret Onplo shows once**, such as a new access key's, is kept in the state as a sensitive
  value, since there is no other moment to read it.
* **Every resource is also a data source** of the same name, which reads one by its id.
* **It signs in with a key from Developer Access**, as `api_key` or in `ONPLO_KEY`. It acts with
  exactly that key's permissions and has no credential of its own.
* **`api_origin`**, or `ONPLO_API_ORIGIN`, sends the same calls to another origin, such as
  staging. Plain http is refused except on this machine, the same rule the command line follows.

## The resources

| App | Resources |
|---|---|
| Account | `onplo_account_team`, and the settings `onplo_account_guest_defaults`, `onplo_account_notification_policy`, `onplo_account_entering_gate` |
| AI | `onplo_ai_provider_profile`, `onplo_ai_agent`, `onplo_ai_memory_store`, `onplo_ai_budget`, and the setting `onplo_ai_default_provider_and_model` |
| Storage | the setting `onplo_storage_retention` |
| Keys | `onplo_keys_key_ring`, and the settings `onplo_keys_company_key_rules`, `onplo_keys_autofill_settings` |
| Integrations | `onplo_integrations_sign_in_app`, `onplo_integrations_access_key`, and the setting `onplo_integrations_connection_rules` |
| Software | `onplo_software_project_in_own_folder`, `onplo_software_project_in_company_folder` |

## How it is checked

On our own checks machine, never on a laptop (`tools/verifiers/go_checks.sh`). The tests send the
plugin protocol's own calls, the ones OpenTofu makes, to the provider built from the real
contracts, against a stand-in Onplo:

* an agent made, read, changed in place and removed;
* a refusal shown in Onplo's words;
* an agent removed in Onplo dropping out of the state;
* a setting set, and left as it is when removed;
* every row of the table matching an operation that exists.

**And OpenTofu itself runs against the same stand-in** (`acceptance_test.go`, ONPLO-5240): the job
sets `TF_ACC=1` and hands the tests the OpenTofu pinned in `tools/verifiers/pinned-toolchains.tsv`,
so a fault only OpenTofu's own planning shows is caught too. Each apply is followed by a plan that
must be empty:

* an agent made, renamed in place, imported by its id and destroyed;
* a refusal reaching the person in Onplo's words;
* a setting set and read back with nothing left to change.

Without `TF_ACC` those three are skipped, and the protocol tests still run.

## Not done yet

* **A connection** (Integrations). Its create body is one shape per kind of system, with that
  system's credentials in it, and a fixed resource cannot hold a choice of shapes.
* **The first signed release in both registries.** The release robot and the copy's own signed
  release are built; what is left is a person's: the robot's GitHub App, the signing key in the
  copy's `release` environment, and the provider registered at both registries.
* **The registry's documentation pages.**

The work: https://linear.app/onplo/issue/ONPLO-5161
