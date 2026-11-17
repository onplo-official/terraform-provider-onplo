// Package operation describes one call on Onplo's public API as data. The generator writes one
// Domain per outsiders' OpenAPI document; the command line builds its commands from them, and the
// OpenTofu provider its resources.
package operation

// Parameter is one path, query or header parameter of an operation.
type Parameter struct {
	Name     string
	In       string // "path", "query" or "header"
	Required bool
}

// The kinds of value a Field holds.
const (
	KindString  = "string"
	KindInteger = "integer"
	KindNumber  = "number"
	KindBoolean = "boolean"
	KindList    = "list"   // Element says what each item is
	KindObject  = "object" // Fields says what it holds
	// KindJSON is a value the contract allows in more than one shape (a oneOf, a free-form map),
	// which no fixed type can hold. It is carried as JSON text.
	KindJSON = "json"
)

// Field is one property of a JSON body, as the contract writes it.
type Field struct {
	Name        string
	Kind        string
	Required    bool   // the contract lists it as required
	Sensitive   bool   // the contract marks it writeOnly or as a password
	Description string // its first paragraph, for the top-level fields of a body only
	Element     *Field  // for KindList
	Fields      []Field // for KindObject
}

// Operation is one call: its id from the contract, and what a request to it needs.
type Operation struct {
	ID         string // the contract's operationId, for example "readMe"
	Method     string // "GET", "POST" and so on
	Path       string // relative to the domain's server, for example "/me"
	Summary    string
	Parameters []Parameter
	HasBody    bool // the operation takes a JSON request body
	SignedOut  bool // the contract says it needs no credential (security: [])
	Body       []Field // the request body's fields, when the body is a JSON object
	Answer     []Field // the success answer's fields, when the answer is a JSON object
}

// Domain is every operation of one outsiders' OpenAPI document.
type Domain struct {
	Name       string // the folder under api-contracts/, for example "account"
	Server     string // the contract's one server address, for example "https://api.onplo.com/v1"
	Operations []Operation
}
