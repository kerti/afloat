// Package api is the Go surface of contract/openapi.yaml: the types and server
// interfaces oapi-codegen generates (api.gen.go) and the contract itself.
package api

import (
	_ "embed"

	"github.com/getkin/kin-openapi/openapi3"
)

// openapi.yaml is a verbatim copy of contract/openapi.yaml, written by
// `make gen-oapi` because //go:embed cannot reach a parent directory, and
// diffed against the contract by scripts/check-generated.sh. It replaces
// oapi-codegen's embedded-spec, whose deflated base64 a secret scanner
// cannot read and, on some contract edits, reads as a credential.
//
//go:embed openapi.yaml
var contractYAML []byte

// GetSpec parses the embedded contract. Each call returns a fresh document,
// so a caller may mutate its copy (httpserver's validator strips security).
func GetSpec() (*openapi3.T, error) {
	return openapi3.NewLoader().LoadFromData(contractYAML)
}
