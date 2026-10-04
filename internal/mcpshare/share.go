// Package mcpshare holds the small helpers both MCP adapters use.
// The SDK types stay in each adapter.
package mcpshare

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ClientName formats a client name and version as one string.
func ClientName(name, version string) string {
	if version == "" {
		return name
	}
	return name + "/" + version
}

// ShortHash is the first 8 bytes of SHA-256, hex encoded.
func ShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

// ClientFault reports whether a JSON-RPC code means the request itself was malformed.
func ClientFault(code int64) bool {
	switch code {
	case -32700, -32600, -32601, -32602:
		return true
	}
	return false
}

// StringsOf lists string values nested in v. A value shorter than 3 characters
// is skipped, so a one-letter argument does not hide an unrelated error.
func StringsOf(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		if len(x) >= 3 {
			out = append(out, x)
		}
	case map[string]any:
		for _, item := range x {
			out = append(out, StringsOf(item)...)
		}
	case []any:
		for _, item := range x {
			out = append(out, StringsOf(item)...)
		}
	}
	return out
}

// HideQuoted replaces err when its text contains one of values, unless content
// is set. A nil error stays nil.
func HideQuoted(err error, values []string, content bool) error {
	if err == nil || content {
		return err
	}
	text := err.Error()
	for _, value := range values {
		if strings.Contains(text, value) {
			return errors.New("mcp handler error")
		}
	}
	return err
}
