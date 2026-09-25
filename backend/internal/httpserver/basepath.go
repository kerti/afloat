package httpserver

import (
	"errors"
	"fmt"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/kerti/afloat/backend/internal/api"
)

// basePath is the prefix every generated route mounts under: the contract's
// servers[0].url, read from the embedded spec rather than spelled out here, so
// the spec owns it in both backends (#17). Kotlin's generator bakes the same
// entry into its controllers' @RequestMapping; oapi-codegen emits nothing for
// it, which is why this reads it at startup. Editing servers[].url used to move
// Kotlin alone.
//
// Resolved at package init: a spec with no usable entry fails the process at
// boot, as a corrupt embedded spec already does (openapiRequestValidator).
var basePath = mustBasePath()

func mustBasePath() string {
	spec, err := api.GetSpec()
	if err != nil {
		panic("httpserver: load embedded openapi spec: " + err.Error())
	}
	p, err := basePathOf(spec)
	if err != nil {
		panic("httpserver: " + err.Error())
	}
	return p
}

// basePathOf accepts only what a mount point can be: an absolute path, with no
// host, no trailing slash and no server variable.
func basePathOf(spec *openapi3.T) (string, error) {
	if len(spec.Servers) == 0 || spec.Servers[0] == nil {
		return "", errors.New("the contract declares no servers entry, so no base path")
	}
	u := spec.Servers[0].URL
	if !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") || strings.HasSuffix(u, "/") ||
		strings.ContainsAny(u, "{}?#") {
		return "", fmt.Errorf("servers[0].url %q is not a base path like /api", u)
	}
	return u, nil
}
