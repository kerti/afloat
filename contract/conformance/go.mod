// The cross-backend HTTP conformance harness (issue #28). Its own module, on
// purpose: it imports neither backend, and a separate go.mod is what makes that
// structural rather than a rule people remember. If this ever imports
// github.com/kerti/afloat/backend/internal/..., the thing being tested has
// leaked into the thing doing the testing.
module github.com/kerti/afloat/conformance

go 1.27

require go.yaml.in/yaml/v3 v3.0.5
