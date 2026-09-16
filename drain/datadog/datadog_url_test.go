package datadog

import "testing"

// TestIntakeURL proves the site selection builds the intake host and keeps a full URL
// when one is given.
func TestIntakeURL(t *testing.T) {
	cases := []struct {
		site string
		want string
	}{
		{"datadoghq.com", "https://http-intake.logs.datadoghq.com/api/v2/logs"},
		{"datadoghq.eu", "https://http-intake.logs.datadoghq.eu/api/v2/logs"},
		{"https://datadoghq.eu/", "https://http-intake.logs.datadoghq.eu/api/v2/logs"},
	}
	for _, tc := range cases {
		if got := intakeURL(tc.site); got != tc.want {
			t.Errorf("intakeURL(%q) = %q, want %q", tc.site, got, tc.want)
		}
	}
}
