package redact_test

import (
	"fmt"

	"github.com/jeremygprawira/wlog/redact"
)

func ExampleNew() {
	r := redact.MustNew(redact.AddKeys("nik"))
	event := map[string]any{
		"password": "hunter2",
		"nik":      "3171010101010001",
		"name":     "alice",
	}
	r.Apply(event)
	fmt.Println(event["password"], event["nik"], event["name"])
	// Output: [REDACTED] [REDACTED] alice
}

func ExampleRedactor_With() {
	base := redact.MustNew(redact.ReplaceKeys("token"))
	derived, err := base.With(redact.AddKeys("nik"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	event := map[string]any{"nik": "3171010101010001"}
	derived.Apply(event)
	fmt.Println(event["nik"])
	// Output: [REDACTED]
}

func ExamplePattern_replace() {
	r := redact.MustNew(redact.AddPatterns(redact.Pattern{
		Name:  "order_ref",
		Regex: `ORD-\d+`,
		Replace: func(m redact.Match) string {
			return "ORD-***"
		},
	}))
	event := map[string]any{"ref": "see ORD-4821 for details"}
	r.Apply(event)
	fmt.Println(event["ref"])
	// Output: see ORD-*** for details
}
