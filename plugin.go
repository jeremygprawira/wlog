package wlog

import (
	"context"
	"fmt"
)

// Plugin is the minimal contract every plugin satisfies: a name, used to identify it
// in OnError reports. A plugin opts into behavior by also implementing any of Setup,
// Enricher, Keeper, Drain, RequestStarter, or RequestFinisher — pick only the hooks it
// needs, in one struct, instead of wiring several separate options.
type Plugin interface {
	Name() string
}

// Setup runs once, at New(), after every other option has been applied. A returned
// error is reported via OnError (source: the plugin's Name()) rather than failing
// New(), which has no error return in its signature.
type Setup interface {
	Setup(l *Logger) error
}

// RequestStarter runs at the start of an HTTP request, before the handler. It is a
// hook point for http-std and its framework adapters; core itself never calls it.
type RequestStarter interface {
	OnRequestStart(ctx context.Context) context.Context
}

// RequestFinisher runs at the end of an HTTP request, right before its event is
// emitted. Like RequestStarter, http-std calls this — core only defines and exposes
// it. It takes only ctx (not the final event map): the map is core-internal at the
// point http-std's handler wrapper returns, so a finisher observes side effects via
// ctx or its own state (e.g. a metrics counter), not the rendered event. Use an
// Enricher instead to add or read fields on the event itself.
type RequestFinisher interface {
	OnRequestFinish(ctx context.Context)
}

// WithPlugins registers plugins. Each is wired into every hook it implements
// (Setup, Enricher, Keeper, Drain) right after all options have run, so a plugin's
// Setup sees the fully configured Logger regardless of option order.
func WithPlugins(plugins ...Plugin) Option {
	return func(l *Logger) { l.plugins = append(l.plugins, plugins...) }
}

// Plugins returns every registered plugin, so an HTTP adapter (http-std and friends)
// can find the ones implementing RequestStarter/RequestFinisher via a type assertion.
func (l *Logger) Plugins() []Plugin {
	return l.plugins
}

// wirePlugins runs each plugin's Setup (if any) and registers it as an Enricher,
// Keeper (only if no WithSampler already set one), and/or Drain, for whichever of
// those it implements. Called once, from New, after every Option has run.
func (l *Logger) wirePlugins() {
	for _, p := range l.plugins {
		l.safeSetup(p)
		if en, ok := p.(Enricher); ok {
			l.enrichers = append(l.enrichers, en)
		}
		if k, ok := p.(Keeper); ok && l.sampler == nil {
			l.sampler = k
		}
		if d, ok := p.(Drain); ok {
			l.drains = append(l.drains, d)
		}
	}
}

func (l *Logger) safeSetup(p Plugin) {
	defer func() {
		if r := recover(); r != nil {
			l.reportError(fmt.Errorf("panic: %v", r), sourceName(p))
		}
	}()
	s, ok := p.(Setup)
	if !ok {
		return
	}
	if err := s.Setup(l); err != nil {
		l.reportError(err, sourceName(p))
	}
}

// sourceName names v for an OnError report: its Plugin.Name() if it has one,
// otherwise its Go type, so a plain Enricher/Keeper/Drain (not a Plugin) still gets a
// useful, if less friendly, label.
func sourceName(v any) string {
	if p, ok := v.(interface{ Name() string }); ok {
		return p.Name()
	}
	return fmt.Sprintf("%T", v)
}
