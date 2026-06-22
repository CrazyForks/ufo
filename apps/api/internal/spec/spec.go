// Package spec holds the embedded OpenAPI contract the Hub serves at /openapi.yaml.
package spec

import _ "embed"

//go:embed openapi.yaml
var Spec []byte
