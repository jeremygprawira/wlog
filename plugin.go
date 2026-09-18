package wlog

import (
	"context"
	"fmt"
)

// Plugin is the minimal contract every plugin satisfies: a name, used to identify it
// in an OnProblem report. A plugin opts into behavior by also implementing any of Setup,
// Starter, Finisher, Enricher, Keeper, Measurer, or Drain — pick only the hooks it
// needs, in one struct, instead of wiring several separate options.
type Plugin interface {
	Name() string
}

// Setup runs once, at New(), after every other option has been applied. A drain may
// implement it too, so a drain that needs the configured Logger gets it once. A
// returned error is reported via OnProblem (source: the hook's Name()) rather than
// failing New(), which has no error return in its signature.
type Setup interface {
	Setup(l *Logger) error
}

// Starter runs at the start of every unit of work, after the event exists. It returns
// the context the unit continues with, so a logger bridge binds a per-unit logger and
// logr.FromContext or zerolog.Ctx folds into the event with no app code.
type Starter interface {
	OnStart(ctx context.Context, kind string) context.Context
}

// Finisher runs at the end of every unit of work, after finalize and before the drains.
// It receives the read-only event, so it sees the summary, the outcome, and the redacted
// fields.
type Finisher interface {
	OnFinish(ctx context.Context, event Event)
}

// WithPlugins registers plugins. Each is wired into every hook it implements
// (Setup, Starter, Finisher, Enricher, Keeper, Measurer, Drain) right after all options
// have run, so a plugin's Setup sees the fully configured Logger regardless of option
// order.
func WithPlugins(plugins ...Plugin) Option {
	return func(l *Logger) { l.plugins = append(l.plugins, plugins...) }
}

// Plugins returns every registered plugin, so an adapter that needs a plugin's own
// state can find it.
func (l *Logger) Plugins() []Plugin {
	return append([]Plugin(nil), l.plugins...)
}

// wirePlugins runs each plugin's Setup (if any) and registers it as a Starter,
// Finisher, Enricher, Keeper, Measurer, and/or Drain, for whichever of those it
// implements. Called once, from New, after every Option has run.
func (l *Logger) wirePlugins() {
	for _, p := range l.plugins {
		l.safeSetup(p)
		if s, ok := p.(Starter); ok {
			l.starters = append(l.starters, s)
		}
		if f, ok := p.(Finisher); ok {
			l.finishers = append(l.finishers, f)
		}
		if en, ok := p.(Enricher); ok {
			l.enrichers = append(l.enrichers, en)
		}
		if k, ok := p.(Keeper); ok {
			l.keepers = append(l.keepers, k)
		}
		if m, ok := p.(Measurer); ok {
			l.measurers = append(l.measurers, m)
		}
		if d, ok := p.(Drain); ok {
			l.drains = append(l.drains, d)
		}
	}
}

// setupDrains calls Setup on every registered drain, so a drain that needs the fully
// configured Logger gets it once, at New. Called after wirePlugins, because a plugin
// may register a drain of its own.
func (l *Logger) setupDrains() {
	for _, d := range l.drains {
		l.safeSetup(d)
	}
}

// safeSetup runs v's Setup hook, if it has one, under recover.
func (l *Logger) safeSetup(v any) {
	defer func() {
		if r := recover(); r != nil {
			l.reportProblem(codeHookPanic, sourceName(v), fmt.Errorf("panic: %v", r))
		}
	}()
	s, ok := v.(Setup)
	if !ok {
		return
	}
	if err := s.Setup(l); err != nil {
		l.reportProblem(codeInvalidConfig, sourceName(v), err)
	}
}

// sourceName names v for an OnProblem report: its Plugin.Name() if it has one,
// otherwise its Go type, so a plain Enricher/Keeper/Drain (not a Plugin) still gets a
// useful, if less friendly, label.
func sourceName(v any) string {
	if p, ok := v.(interface{ Name() string }); ok {
		return p.Name()
	}
	return fmt.Sprintf("%T", v)
}
