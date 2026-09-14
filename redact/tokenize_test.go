package redact

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	cases := []struct {
		key  string
		want []string
	}{
		{"auth", []string{"auth"}},
		{"author", []string{"author"}},
		{"accessToken", []string{"access", "token"}},
		{"X-Auth-Token", []string{"x", "auth", "token"}},
		{"HTTPAuthToken", []string{"http", "auth", "token"}},
		{"tokenizer_version", []string{"tokenizer", "version"}},
		{"user_password", []string{"user", "password"}},
		{"login_pin", []string{"login", "pin"}},
		{"spin_count", []string{"spin", "count"}},
		{"api-key", []string{"api", "key"}},
		{"", nil},
		{"a1b2", []string{"a1b2"}},
		{"日本語Token", []string{"日本語", "token"}},
	}
	for _, c := range cases {
		got := tokenize(c.key)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("tokenize(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}
