// This file tests the syslog configuration from inside the package, because the resolved
// address, the facility check, and the cloned TLS config are unexported. The external test
// only proves what a caller can see.
package syslog

import (
	"crypto/tls"
	"testing"
)

// TestSyslog_D14_RefusesAFacilityOutsideTheRange proves a facility outside 0 to 23 fails at
// construction, because the PRI would be invalid.
func TestSyslog_D14_RefusesAFacilityOutsideTheRange(t *testing.T) {
	for _, facility := range []Facility{-1, 24, 99} {
		if _, _, err := newSender(WithAddr("collector.example"), WithFacility(facility)); err == nil {
			t.Errorf("facility %d was accepted", facility)
		}
	}
	if _, _, err := newSender(WithAddr("collector.example"), WithFacility(23)); err != nil {
		t.Errorf("facility 23 was refused: %v", err)
	}
}

// TestSyslog_D15_TLSDefaultPort proves a TLS address with no port uses 6514, and an address
// with one keeps it.
func TestSyslog_D15_TLSDefaultPort(t *testing.T) {
	s, _, err := newSender(WithAddr("collector.example"), WithNetwork("tls"))
	if err != nil {
		t.Fatalf("newSender: %v", err)
	}
	if s.addr != "collector.example:6514" {
		t.Errorf("addr = %q, want collector.example:6514", s.addr)
	}
	s, _, err = newSender(WithAddr("collector.example:10514"), WithNetwork("tls"))
	if err != nil {
		t.Fatalf("newSender: %v", err)
	}
	if s.addr != "collector.example:10514" {
		t.Errorf("addr = %q, want the port the caller named", s.addr)
	}
}

// TestSyslog_D16_ClonesTheTLSConfig proves the floor is set on a copy, so the caller's
// config is unchanged.
func TestSyslog_D16_ClonesTheTLSConfig(t *testing.T) {
	cfg := &tls.Config{}
	if _, _, err := newSender(WithAddr("collector.example"), WithNetwork("tls"), WithTLSConfig(cfg)); err != nil {
		t.Fatalf("newSender: %v", err)
	}
	if cfg.MinVersion != 0 {
		t.Errorf("the caller's MinVersion = %d, want it unchanged", cfg.MinVersion)
	}
}
