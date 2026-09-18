// Package schema holds the JSON Schemas of the documents wlog writes: the event shape,
// and the map report.
//
// A schema is embedded rather than generated, because the shape is a table in a spec and
// a reader must be able to read both side by side. tools/cmd/schema validates every
// golden document against its schema, so a drift fails the build.
package schema

import _ "embed"

// eventV1 is the JSON Schema of one event, as SPEC-core-v2 defines it.
//
//go:embed event.v1.json
var eventV1 []byte

// mapV2 is the JSON Schema of the document wlog map writes.
//
//go:embed map.v2.json
var mapV2 []byte

// EventV1 returns the JSON Schema of one event.
func EventV1() []byte { return eventV1 }

// MapV2 returns the JSON Schema of the map report.
func MapV2() []byte { return mapV2 }
