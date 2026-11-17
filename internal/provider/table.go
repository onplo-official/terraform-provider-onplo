package provider

// WHICH OPERATIONS ARE CONFIGURATION. Every resource below is a few operation ids of one app's
// contract and nothing more: its fields, which of them can change in place, and which are only
// read come from the contract, through the shared client's operations table. So a field added to a
// contract is in the provider the day it lands. What cannot come from a contract is the judgement
// this list makes: that a team, an agent or a retention period is configuration someone keeps in
// a file, and a file, a chat or a job is content that is not (ONPLO-5161).
//
// A row with no Create is a setting: one per environment or per person, which always exists. Its
// Update both creates and changes it, and removing it from the file leaves it as it is.
var table = []spec{
	{Domain: "account", Name: "account_team", Create: "createTeam", Read: "listTeams", Update: "updateTeam", Delete: "deleteTeam"},
	{Domain: "account", Name: "account_guest_defaults", Read: "getGuestDefaults", Update: "setGuestDefaults"},
	{Domain: "account", Name: "account_notification_policy", Read: "getNotificationPolicy", Update: "setNotificationPolicy"},
	{Domain: "account", Name: "account_entering_gate", Read: "getEnteringGate", Update: "setEnteringGate"},

	{Domain: "ai", Name: "ai_provider_profile", Create: "createProviderProfile", Read: "getProviderProfile", Update: "updateProviderProfile", Delete: "removeProviderProfile"},
	{Domain: "ai", Name: "ai_agent", Create: "createAgent", Read: "getAgent", Update: "updateAgent", Delete: "deleteAgent"},
	{Domain: "ai", Name: "ai_memory_store", Create: "createMemoryStore", Read: "getMemoryStore", Update: "updateMemoryStore", Delete: "deleteMemoryStore"},
	// A skill has no row: it is a SKILL.md folder in Storage, and AI keeps no copy of its own to
	// configure (decision 1036).
	{Domain: "ai", Name: "ai_budget", Create: "createBudget", Read: "getBudget", Update: "updateBudget", Delete: "removeBudget"},
	{Domain: "ai", Name: "ai_default_provider_and_model", Read: "getDefaultProviderAndModel", Update: "setDefaultProviderAndModel"},

	{Domain: "storage", Name: "storage_retention", Read: "getRetention", Update: "setRetention"},

	{Domain: "keys", Name: "keys_key_ring", Create: "createKeyRing", Read: "listKeyRings", Update: "updateKeyRing", Delete: "deleteKeyRing"},
	{Domain: "keys", Name: "keys_company_key_rules", Read: "getCompanyKeyRules", Update: "updateCompanyKeyRules"},
	{Domain: "keys", Name: "keys_autofill_settings", Read: "getKeysAutofillSettings", Update: "setKeysAutofillSettings"},

	// A connection is left out for now: its create body is one shape per kind of system, with
	// the system's credentials in it, and a fixed resource cannot hold a choice of shapes.
	{Domain: "integrations", Name: "integrations_sign_in_app", Create: "registerSignInApp", Read: "getSignInApp", Update: "updateSignInApp", Delete: "revokeSignInApp", ShownOnce: []string{"client_secret"}},
	{Domain: "integrations", Name: "integrations_access_key", Create: "createAccessKey", Read: "getAccessKey", Update: "updateAccessKey", Delete: "revokeAccessKey", ShownOnce: []string{"secret"}},
	{Domain: "integrations", Name: "integrations_connection_rules", Read: "getConnectionRules", Update: "setConnectionRules"},

	{Domain: "software", Name: "software_project_in_own_folder", Create: "markOwnFolderAsSoftwareProject", Read: "getSoftwareProject", Update: "updateSoftwareProject", Delete: "unmarkSoftwareProject"},
	{Domain: "software", Name: "software_project_in_company_folder", Create: "markCompanyFolderAsSoftwareProject", Read: "getSoftwareProject", Update: "updateSoftwareProject", Delete: "unmarkSoftwareProject"},
}

// spec is one row of the table.
type spec struct {
	Domain string // the folder under api-contracts/
	Name   string // the resource is onplo_<Name>
	Create string // "" for a setting
	Read   string // a read of one, or a list the item is found in
	Update string
	Delete string // "" for a setting
	// ShownOnce names the fields of Create's answer that Onplo shows only then, a secret. They are
	// kept in the state, marked sensitive, because there is no other moment to read them.
	ShownOnce []string
}
