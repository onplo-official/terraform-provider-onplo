# onplo-go-client — the Go client the command line and the provider share

**It holds the program that writes Onplo's Go client from the outsiders' OpenAPI documents, and
never the client itself: `go generate ./...` writes that under `generated/`, which git ignores
(decision 0548).** The command line and the OpenTofu provider both import it, so they call one set
of operations with one set of types. The developer-tools lane owns it.

## What `go generate ./...` writes

| Folder | What it is | Who reads it |
|---|---|---|
| `generated/<domain>/` | The typed client and models for one domain, by oapi-codegen | Whoever needs one domain's types |
| `generated/operations/` | Every operation as data: its method, path and parameters, and the fields of its body and its answer | The command line, which makes one command per operation, and the provider, which makes its resources' attributes from the fields |

**Which documents.** Every document in `api-contracts/<domain>/openapi/` whose one server address
is the outsiders' door. A door for our own apps sits under `/our-frontend/` and is left out by that
same address (decision 0813, rule 5). A new outsiders' contract is in the client the day its
document lands, and nothing here changes. The npm SDK applies the same rule.

**Where a call goes.** `origin/` holds the one rule both programs follow: the contract's server,
or the same path on another origin, and plain http only on this machine.

## The generator

* **oapi-codegen v2.8.0**, pinned in `go.mod` and used as a library by `internal/generator/`.
  It is the first version that reads OpenAPI 3.1, which every Onplo contract is written in.
* The update robot proposes newer versions weekly, like every other Go module here.

## How it is checked

On our own checks machine, never on a laptop: the client is generated, then the module is tidied,
vetted and tested (`tools/verifiers/go_checks.sh`). The tests plant a contract of each kind and
prove that only the outsiders' one is written.

## Where to read more

* Why Go, and why this folder: architecture decision records 038 and 039
* The work: https://linear.app/onplo/issue/ONPLO-5160
