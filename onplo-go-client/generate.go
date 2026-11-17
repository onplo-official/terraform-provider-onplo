// Package goclient is Onplo's Go client, written by `go generate ./...` from the outsiders'
// OpenAPI documents in api-contracts/ and never committed (decision 0548). The command line and
// the OpenTofu provider both import it, so they share one set of operations and types.
//
// What `go generate` writes, under generated/:
//
//   - generated/<domain>/: the typed client and its models for one domain, by oapi-codegen.
//   - generated/operations/: every operation as data (its method, path and parameters), which
//     the command line turns into commands.
package goclient

// The generated typed clients import this runtime package, but they are not on disk before
// `go generate` runs. This blank import keeps go.mod and go.sum the same before and after
// generation, so `go mod tidy -diff` gives the same answer either way.
import _ "github.com/oapi-codegen/runtime"

//go:generate go run ./internal/generator -contracts ../../api-contracts -out generated
