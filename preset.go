// This file holds the output preset contract. A preset reshapes the JSON line a
// writer prints, and it never changes the canonical event a drain receives.
package wlog

// OutputPreset changes the JSON that a JSON writer prints. It never changes the event a
// drain, a keeper, or a hook saw, because core hands Apply a copy of the event.
//
// A preset with no Lead key keeps the fixed key order of the default shape. Any other
// preset writes its Lead keys first and in that order, then every other key sorted by
// name.
type OutputPreset interface {
	Name() string                              // default, flat, otel, ecs, gcp, datadog, or emf
	Apply(event map[string]any) map[string]any // a new map, and never a change to event
	Lead() []string                            // top-level keys written first, in this order
}

// configReporter is an optional OutputPreset method. A preset that names a bad
// configuration reports it, so WithOutput can warn and still print a shape.
type configReporter interface{ ConfigError() error }

// WithOutput sets the output preset of the JSON writer. A nil preset keeps the default
// shape. The pretty console always renders the canonical event, because it is a
// developer view and not a backend dialect.
//
// A preset that names a bad configuration reports WLOG_INVALID_CONFIG. A preset that
// panics is recovered at the writer, and the canonical event is printed instead.
func WithOutput(p OutputPreset) Option {
	return func(l *Logger) {
		if bad, ok := p.(configReporter); ok {
			if err := bad.ConfigError(); err != nil {
				l.reportProblem(codeInvalidConfig, "WithOutput", err)
			}
		}
		l.output = p
	}
}
