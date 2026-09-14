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
	cases := []string{"+62 812-3456-7890", "081234567890"}
	for _, in := range cases {
		event := map[string]any{"phone": in}
		r.Apply(event)
		if event["phone"] != "+62 ****7890" {
			t.Errorf("phone %q: got %q", in, event["phone"])
		}
	}
}

func TestRedact_Builtin_IBAN(t *testing.T) {
	r := redact.MustNew()
	event := map[string]any{"iban": "FR76 3000 6000 0112 3456 7890 189"}
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
