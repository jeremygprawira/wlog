// Package enrich provides built-in wlog.Enrichers for context every event benefits
// from: host/deployment metadata, a parsed user agent, geo from CDN headers, and a
// user-id lookup — evlog's enricher set, in Go.
package enrich

// Option configures Host, Deployment, and Geo. They share one option type since their
// only real setting is the same: whether to overwrite a field the caller already set.
type Option func(*config)

type config struct {
	overwrite  bool
	geoHeaders map[string]string
}

func newConfig(opts []Option) config {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c
}

// Overwrite controls whether an enricher replaces a field the event already has.
// Default false: an enricher never overwrites data the request or handler set.
func Overwrite(on bool) Option {
	return func(c *config) { c.overwrite = on }
}

// mergeGroup adds fields into event[group] (creating it if absent), skipping any key that
// already exists there unless overwrite is set.
//
// A group key the event already holds as something other than a map is left exactly as it is:
// an enricher adds context, and it never changes what the app itself recorded (gate G3).
func mergeGroup(event map[string]any, group string, fields map[string]any, overwrite bool) {
	if existing, ok := event[group]; ok && existing != nil {
		if _, isMap := existing.(map[string]any); !isMap {
			return
		}
	}
	g, ok := event[group].(map[string]any)
	if !ok {
		g = map[string]any{}
		event[group] = g
	}
	for k, v := range fields {
		if !overwrite {
			if _, exists := g[k]; exists {
				continue
			}
		}
		g[k] = v
	}
}
