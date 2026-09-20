// Package explain holds the one registry of ids a reader can ask wlog about. Every entry
// comes from the source that the runtime uses, so the answer and the behavior never
// drift.
//
// Read top to bottom: Entry is one answer. All builds the registry from the problem
// codes, the map rules, the doctor codes, the reserved fields, and the environment
// variables. Find answers one id, and Closest names the near ids of an unknown one.
package explain

import (
	"sort"
	"strings"

	"github.com/jeremygprawira/wlog"
	wlogdoctor "github.com/jeremygprawira/wlog/cmd/wlog/cmd/doctor"
	"github.com/jeremygprawira/wlog/cmd/wlog/rules"
	"github.com/jeremygprawira/wlog/setup"
)

// Entry is one answer of wlog explain.
type Entry struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	What    string `json:"what"`
	Why     string `json:"why,omitempty"`
	Fix     string `json:"fix,omitempty"`
	Example string `json:"example,omitempty"`
	Link    string `json:"link,omitempty"`
}

// coreVars are the wlog settings the core reads at startup.
var coreVars = []string{"WLOG_DRAINS", "WLOG_OUTPUT", "WLOG_SERVICE", "WLOG_VERSION", "WLOG_ENV", "WLOG_LEVEL", "WLOG_DEBUG"}

// All returns every entry, in id order.
func All() []Entry {
	out := []Entry{}
	for _, problem := range wlog.Problems() {
		out = append(out, Entry{
			ID: problem.Code, Kind: "problem", What: problem.Message,
			Why: problem.Why, Fix: problem.Fix, Link: problem.Link,
		})
	}
	for id, info := range rules.Infos {
		out = append(out, Entry{
			ID: id, Kind: "rule",
			What: "the map rule " + id,
			Why:  "the rule protects one observability habit of a handler",
			Fix:  info.Fix,
			Link: "https://github.com/jeremygprawira/wlog/blob/main/docs/rules.md#" + info.Docs,
		})
	}
	for _, code := range wlogdoctor.Codes() {
		out = append(out, Entry{
			ID: code, Kind: "doctor",
			What: "a wlog doctor finding code",
			Why:  "the doctor reports the module, the middleware, the drains, and the redactor under one code",
			Fix:  "run wlog doctor and follow the fix line of the finding",
		})
	}
	for _, field := range wlog.ReservedFields() {
		out = append(out, Entry{
			ID: field, Kind: "field",
			What: "a reserved field of the event shape",
			Why:  "a reader of every event knows the field and its meaning",
			Fix:  "do not write the field through wlog.Set, because core owns it",
			Link: "https://github.com/jeremygprawira/wlog/blob/main/docs/event-shape.md",
		})
	}
	out = append(out, envEntries()...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// envEntries returns one entry per environment variable the runtime reads. It holds no
// value, so a secret never lands in the answer.
func envEntries() []Entry {
	out := make([]Entry, 0, len(coreVars)+8)
	for _, name := range coreVars {
		out = append(out, Entry{
			ID: name, Kind: "env",
			What: "a wlog setting read at startup",
			Fix:  "set the variable in the environment of the app",
		})
	}
	for _, factory := range setup.Builtins() {
		for _, variable := range factory.Vars {
			out = append(out, Entry{
				ID: variable.Name, Kind: "env",
				What: "the " + factory.Name + " drain reads it",
				Fix:  "set the variable, or leave it unset to disable the drain",
			})
			for _, alias := range variable.Aliases {
				out = append(out, Entry{
					ID: alias, Kind: "env",
					What: "an alias of " + variable.Name + " for the " + factory.Name + " drain",
					Fix:  "set the variable, or leave it unset to disable the drain",
				})
			}
		}
	}
	return out
}

// Find returns the entry of one id.
func Find(id string) (Entry, bool) {
	for _, entry := range All() {
		if entry.ID == id {
			return entry, true
		}
	}
	return Entry{}, false
}

// Closest returns up to n ids near one unknown id, nearest first.
func Closest(id string, n int) []string {
	type scored struct {
		id       string
		distance int
	}
	scoredIDs := make([]scored, 0, 64)
	for _, entry := range All() {
		if entry.ID == id {
			continue
		}
		scoredIDs = append(scoredIDs, scored{id: entry.ID, distance: distance(id, entry.ID)})
	}
	sort.SliceStable(scoredIDs, func(i, j int) bool {
		if scoredIDs[i].distance != scoredIDs[j].distance {
			return scoredIDs[i].distance < scoredIDs[j].distance
		}
		return scoredIDs[i].id < scoredIDs[j].id
	})
	out := make([]string, 0, n)
	for _, item := range scoredIDs {
		if len(out) == n {
			break
		}
		out = append(out, item.id)
	}
	return out
}

// distance returns the edit distance between two ids, which names the near ids of a
// typo.
func distance(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// min returns the smallest of three numbers.
func min(a, b, c int) int {
	smallest := a
	if b < smallest {
		smallest = b
	}
	if c < smallest {
		smallest = c
	}
	return smallest
}
