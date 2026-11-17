package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/util"
)

const outsiders = `openapi: 3.1.0
info: {title: Planted, version: '1'}
servers: [{url: 'https://api.example.test/v1'}]
security: [{bearerAuth: []}]
components:
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
paths:
  /things/{thing_id}:
    parameters:
      - {name: thing_id, in: path, required: true, schema: {type: string}}
    get:
      operationId: readThing
      summary: Read one thing
      responses: {'200': {description: The thing.}}
  /status:
    get:
      operationId: readStatus
      security: []
      responses: {'200': {description: Up.}}
`

const ourFrontend = `openapi: 3.1.0
info: {title: Planted screens, version: '1'}
servers: [{url: 'https://api.example.test/our-frontend/v1'}]
paths:
  /screen:
    get:
      operationId: readScreen
      responses: {'200': {description: A screen.}}
`

func plant(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The normal case: the outsiders' document is written, the one for our own apps never is.
func TestTheOutsidersDocumentIsWrittenAndOurFrontendsIsNot(t *testing.T) {
	root := plant(t, map[string]string{
		"things/openapi/things.yaml":           outsiders,
		"things/openapi/for-our-frontend.yaml": ourFrontend,
	})
	out := filepath.Join(t.TempDir(), "generated")
	if err := run(root, out); err != nil {
		t.Fatal(err)
	}
	client, err := os.ReadFile(filepath.Join(out, "things", "client.gen.go"))
	if err != nil || !strings.Contains(string(client), "ReadThing") {
		t.Fatalf("the typed client was not written with ReadThing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "thingsforourfrontend")); !os.IsNotExist(err) {
		t.Fatal("the door for our own apps was written into a published client")
	}
	table, err := os.ReadFile(filepath.Join(out, "operations", "operations.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"readThing"`, `"thing_id"`, `"readStatus"`} {
		if !strings.Contains(string(table), want) {
			t.Errorf("the operations table lacks %s:\n%s", want, table)
		}
	}
	if strings.Contains(string(table), "readScreen") {
		t.Error("a per-screen read reached the operations table")
	}
}

// The main failure: a contract that names no single server is refused, never guessed.
func TestAContractWithoutOneServerIsRefused(t *testing.T) {
	root := plant(t, map[string]string{
		"things/openapi/things.yaml": strings.Replace(outsiders,
			"servers: [{url: 'https://api.example.test/v1'}]",
			"servers: [{url: 'https://a.example.test/v1'}, {url: 'https://b.example.test/v1'}]", 1),
	})
	_, err := outsidersDocuments(root, util.LoadSwagger)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("want a refusal naming the rule, got %v", err)
	}
}

func TestSignedOutFollowsTheContract(t *testing.T) {
	root := plant(t, map[string]string{"things/openapi/things.yaml": outsiders})
	docs, err := outsidersDocuments(root, util.LoadSwagger)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := domainOf(docs[0])
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, op := range domain.Operations {
		got[op.ID] = op.SignedOut
	}
	if got["readThing"] || !got["readStatus"] {
		t.Fatalf("signed-out flags wrong: %v", got)
	}
}

// A contract may name a component the way oapi-codegen names an inline property's type; the
// generator renames the inline one, and the contract is left as written.
func TestAnInlineTypeThatCollidesIsRenamed(t *testing.T) {
	root := plant(t, map[string]string{"things/openapi/things.yaml": outsiders + `    post:
      operationId: answerThing
      requestBody:
        required: true
        content:
          application/json:
            schema: {$ref: '#/components/schemas/ThingAnswer'}
      responses: {'200': {description: Answered., content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}}}
`})
	contract := filepath.Join(root, "things/openapi/things.yaml")
	planted, _ := os.ReadFile(contract)
	withSchemas := strings.Replace(string(planted), "    bearerAuth: {type: http, scheme: bearer}\n",
		"    bearerAuth: {type: http, scheme: bearer}\n"+
			"  schemas:\n"+
			"    ThingAnswer: {type: object, required: [answer], properties: {answer: {type: string, enum: [approve, deny]}}}\n"+
			"    Thing: {type: object, required: [answer], properties: {answer: {anyOf: [{type: string, enum: [approve, deny]}, {type: 'null'}]}}}\n", 1)
	if err := os.WriteFile(contract, []byte(withSchemas), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "generated")
	if err := run(root, out); err != nil {
		t.Fatal(err)
	}
	client, _ := os.ReadFile(filepath.Join(out, "things", "client.gen.go"))
	if !strings.Contains(string(client), "ThingAnswerValue") {
		t.Fatal("the inline answer was not renamed ThingAnswerValue")
	}
	after, _ := os.ReadFile(contract)
	if strings.Contains(string(after), "x-go") {
		t.Fatal("the contract on disk was changed")
	}
}

// The provider reads a body's fields from the table: a described allOf is looked through, a value
// that may be null keeps its type, and a union of shapes is carried as JSON.
func TestABodysFieldsAreRead(t *testing.T) {
	root := plant(t, map[string]string{"things/openapi/things.yaml": strings.Replace(outsiders,
		"    bearerAuth: {type: http, scheme: bearer}\n",
		"    bearerAuth: {type: http, scheme: bearer}\n"+
			"  schemas:\n"+
			"    Name: {type: string}\n"+
			"    Thing:\n"+
			"      type: object\n"+
			"      required: [name]\n"+
			"      properties:\n"+
			"        name: {allOf: [{$ref: '#/components/schemas/Name'}], description: \"What it is called.\\n\\nMore.\"}\n"+
			"        size: {anyOf: [{type: integer}, {type: 'null'}]}\n"+
			"        tags: {type: array, items: {type: string}}\n"+
			"        shape: {oneOf: [{type: string}, {type: integer}]}\n"+
			"        secret: {type: string, writeOnly: true}\n", 1) + `    patch:
      operationId: changeThing
      requestBody: {content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}}
      responses: {'200': {description: Changed., content: {application/json: {schema: {$ref: '#/components/schemas/Thing'}}}}}
`})
	docs, err := outsidersDocuments(root, util.LoadSwagger)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := domainOf(docs[0])
	if err != nil {
		t.Fatal(err)
	}
	var body []string
	for _, op := range domain.Operations {
		if op.ID != "changeThing" {
			continue
		}
		if len(op.Answer) != 5 {
			t.Errorf("the answer has %d fields, want 5", len(op.Answer))
		}
		for _, f := range op.Body {
			entry := f.Name + ":" + f.Kind
			if f.Required {
				entry += ":required"
			}
			if f.Sensitive {
				entry += ":sensitive"
			}
			if f.Element != nil {
				entry += ":" + f.Element.Kind
			}
			body = append(body, entry)
			if f.Name == "name" && f.Description != "What it is called." {
				t.Errorf("name's description is %q", f.Description)
			}
		}
	}
	got := strings.Join(body, " ")
	want := "name:string:required secret:string:sensitive shape:json size:integer tags:list:string"
	if got != want {
		t.Fatalf("body fields\n got %s\nwant %s", got, want)
	}
}
