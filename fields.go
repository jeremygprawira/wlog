package wlog

import "maps"

// FieldNames maps a canonical field path (e.g. "service.env") to the key it should be
// written under. Only reserved/core fields are ever renamed — a user's own Set/
// SetGroup/Append keys are always written under the name the caller gave them.
//
// This covers the reserved fields core produces today; later modules (http-std and
// beyond) extend FieldsFlat/FieldsOTel as they add their own reserved fields.
type FieldNames map[string]string

// WithFieldNames overrides the output name of one or more canonical fields. Repeated
// calls merge; a later WithFieldNames wins on a key both specify.
func WithFieldNames(names FieldNames) Option {
	return func(l *Logger) {
		if l.fieldNames == nil {
			l.fieldNames = FieldNames{}
		}
		maps.Copy(l.fieldNames, names)
	}
}

// FieldsFlat renames fields to match go-echo-boilerplate's flat shape: service,
// version, and environment as top-level keys instead of a nested "service" object.
func FieldsFlat() FieldNames {
	return FieldNames{
		"service.name":    "service",
		"service.version": "version",
		"service.env":     "environment",
	}
}

// FieldsOTel renames fields to OpenTelemetry resource attribute names.
func FieldsOTel() FieldNames {
	return FieldNames{
		"service.name":    "service.name",
		"service.version": "service.version",
		"service.env":     "deployment.environment",
	}
}

// serviceSubfields are the canonical dotted names for the fields nested under "service".
var serviceSubfields = map[string]string{
	"service.name":    "name",
	"service.version": "version",
	"service.env":     "env",
}

// applyFieldNames renames reserved fields per names, run after redaction so denylist
// entries (which match canonical names) are never affected by the output shape. With
// no names configured, out is returned unchanged.
func applyFieldNames(out map[string]any, names FieldNames) map[string]any {
	if len(names) == 0 {
		return out
	}

	if svc, ok := out["service"].(map[string]any); ok {
		renamedCount := 0
		for canonical := range serviceSubfields {
			if _, wants := names[canonical]; wants {
				renamedCount++
			}
		}
		// Delete the old nested object first: a preset may reuse "service" as one of
		// the new flat key names (FieldsFlat does), and writing that value before
		// deleting would let this delete erase the very value it just wrote.
		if renamedCount == len(serviceSubfields) {
			delete(out, "service")
		}
		for canonical, field := range serviceSubfields {
			if newKey, wants := names[canonical]; wants {
				out[newKey] = svc[field]
			}
		}
	}

	for _, canonical := range []string{"operation", "duration_ms", "outcome", "level", "timestamp"} {
		newKey, wants := names[canonical]
		if !wants || newKey == canonical {
			continue
		}
		if v, present := out[canonical]; present {
			out[newKey] = v
			delete(out, canonical)
		}
	}
	return out
}
