// This file holds the environment reader of setup: the Env interface, the two readers
// that ship, and the Var record that documents one variable of one drain.
package setup

import (
	"os"
	"strings"
)

// Env reads one environment variable. os.LookupEnv satisfies it, and a test passes a
// map, so no test reads the process environment.
type Env interface {
	Lookup(name string) (string, bool)
}

// OSEnv reads the process environment.
type OSEnv struct{}

// Lookup returns one variable of the process environment.
func (OSEnv) Lookup(name string) (string, bool) { return os.LookupEnv(name) }

// MapEnv is a map-backed Env, for a test.
type MapEnv map[string]string

// Lookup returns one variable of the map.
func (m MapEnv) Lookup(name string) (string, bool) {
	value, ok := m[name]
	return value, ok
}

// Var documents one environment variable of one drain, so wlog doctor and wlog explain
// env can list it. A Secret value is never printed.
type Var struct {
	Name     string   // WLOG_KAFKA_BROKERS
	Aliases  []string // names read when Name is unset, in order
	Required bool     // a drain with no value for it is skipped
	Secret   bool     // never printed by doctor
}

// first returns the first set value of the names, and whether one was set.
func first(env Env, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := env.Lookup(name); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

// lookup returns the value of the variable, from its own name or from an alias.
func (v Var) lookup(env Env) (string, bool) {
	return first(env, append([]string{v.Name}, v.Aliases...)...)
}

// Value returns the value of the variable, from its own name or from an alias. The name
// wins when both are set.
func (v Var) Value(env Env) (string, bool) { return v.lookup(env) }

// missing returns every name of this variable that holds no value, and nil when one
// name holds a value.
func (v Var) missing(env Env) []string {
	if _, ok := v.lookup(env); ok {
		return nil
	}
	return append([]string{v.Name}, v.Aliases...)
}

// required marks a variable as required.
func required(name string, aliases ...string) Var {
	return Var{Name: name, Aliases: aliases, Required: true}
}

// optional marks a variable as optional.
func optional(name string, aliases ...string) Var {
	return Var{Name: name, Aliases: aliases}
}

// pairs reads a comma-separated list of key=value pairs, such as the header variable of
// OTLP. A pair with no separator is skipped.
func pairs(value string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(value, ",") {
		key, item, found := strings.Cut(pair, "=")
		if found && strings.TrimSpace(key) != "" {
			out[strings.TrimSpace(key)] = strings.TrimSpace(item)
		}
	}
	return out
}
