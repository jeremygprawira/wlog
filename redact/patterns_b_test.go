package redact_test

import (
	"testing"

	"github.com/jeremygprawira/wlog/redact"
)

func TestRedact_Builtin_IPv4(t *testing.T) {
	r := redact.MustNew()

	event := map[string]any{"remote": "client at 192.168.1.100 connected"}
	r.Apply(event)
	if event["remote"] != "client at ***.***.***.100 connected" {
		t.Errorf("ipv4: got %q", event["remote"])
	}

	for _, loopback := range []string{"127.0.0.1", "0.0.0.0"} {
		event := map[string]any{"remote": loopback}
		r.Apply(event)
		if event["remote"] != loopback {
			t.Errorf("loopback %q got masked: %q", loopback, event["remote"])
		}
	}
}

func TestRedact_Builtin_IPv4_ClientIPExempt(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"http": map[string]any{"client_ip": "203.0.113.5"}}
	r.Apply(event)
	got := event["http"].(map[string]any)["client_ip"]
	if got != "203.0.113.5" {
		t.Errorf("http.client_ip masked by default: %v", got)
	}

	masking := redact.MustNew(redact.MaskClientIP())
	event2 := map[string]any{"http": map[string]any{"client_ip": "203.0.113.5"}}
	masking.Apply(event2)
	got2 := event2["http"].(map[string]any)["client_ip"]
	if got2 != "***.***.***.5" {
		t.Errorf("MaskClientIP did not mask http.client_ip: %v", got2)
	}
}

func TestRedact_Builtin_Phone(t *testing.T) {
	r := redact.MustNew()
	// The masker keeps the country code it found and adds none, so a number that
	// arrived without one keeps its local shape.
	cases := map[string]string{
		"+62 812-3456-7890": "+62 ****7890",
		"081234567890":      "****7890",
	}
	for in, want := range cases {
		event := map[string]any{"phone": in}
		r.Apply(event)
		if event["phone"] != want {
			t.Errorf("phone %q: got %q, want %q", in, event["phone"], want)
		}
	}
}

func TestRedact_Builtin_IBAN(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"iban": "FR7630006000011234567890189"}
	r.Apply(event)
	if event["iban"] != "FR76****189" {
		t.Errorf("iban: got %q", event["iban"])
	}
}

func TestRedact_Builtin_NIK_OffByDefault(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"note": "3171010101010001"}
	r.Apply(event)
	if event["note"] != "3171010101010001" {
		t.Errorf("nik masked by default: %q", event["note"])
	}
}

func TestRedact_Builtin_NIK_EnabledByOption(t *testing.T) {
	r := redact.MustNew(redact.EnablePatterns("nik"))
	event := map[string]any{"note": "3171010101010001"}
	r.Apply(event)
	if event["note"] != "3171********0001" {
		t.Errorf("nik: got %q", event["note"])
	}
}

// TestRedact_RED7_NoFalsePositives proves that the value patterns leave ordinary
// text alone: a browser user agent, a millisecond timestamp, a snowflake id, a
// short zero-padded number, and a plain status line stay unchanged.
func TestRedact_RED7_NoFalsePositives(t *testing.T) {
	r := redact.MustNew()

	unchanged := map[string]string{
		"user_agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36",
		"ts_ms":      "1757952000123",
		"snowflake":  "1234567890123456789",
		"zero_pad":   "0000012345",
		"order_note": "ID12 ORDER STATUS OK",
	}
	for key, value := range unchanged {
		event := map[string]any{key: value}
		r.Apply(event)
		if event[key] != value {
			t.Errorf("%s = %v, want it unchanged", key, event[key])
		}
	}

	// A real card stays masked, with separators or under a card key name.
	masked := map[string]any{
		"card":        "4111 1111 1111 1111",
		"number":      "4111-1111-1111-1111",
		"card_number": "4111111111111111",
		"pan":         "378282246310005",
	}
	r.Apply(masked)
	for key, value := range masked {
		if value == nil {
			continue
		}
		raw := map[string]any{"card": "4111 1111 1111 1111", "number": "4111-1111-1111-1111",
			"card_number": "4111111111111111", "pan": "378282246310005"}[key]
		if value == raw {
			t.Errorf("%s = %v, want the card masked", key, value)
		}
	}
}
