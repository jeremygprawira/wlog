package redact

import (
	"regexp"
	"strings"
)

var (
	reIPv4  = regexp.MustCompile(`(?:^|[^A-Za-z0-9/])((?:\d{1,3}\.){3}\d{1,3})\b`)
	rePhone = regexp.MustCompile(`(?:\+\d{1,3}[\d\s-]{6,}\d)|(?:\b0[1-9][\d\s-]{6,}\d\b)`)
	reIBAN  = regexp.MustCompile(`\b[A-Z]{2}\d{2}[A-Z0-9]{13,30}\b`)
	reNIK   = regexp.MustCompile(`\b\d{16}\b`)
)

func maskIPv4(match string) string {
	if match == "127.0.0.1" || match == "0.0.0.0" {
		return match
	}
	parts := strings.Split(match, ".")
	if len(parts) != 4 {
		return match
	}
	return "***.***.***." + parts[3]
}

func maskPhone(match string) string {
	digits := digitsOf(match)
	if len(digits) < 4 {
		return match
	}
	last4 := digits[len(digits)-4:]

	prefix := ""
	switch {
	case strings.HasPrefix(match, "+"):
		i := 1
		for i < len(match) && i <= 3 && match[i] >= '0' && match[i] <= '9' {
			i++
		}
		prefix = match[:i]
	}
	if prefix == "" {
		return "****" + last4
	}
	return prefix + " ****" + last4
}

func maskIBAN(match string) string {
	stripped := strings.ReplaceAll(match, " ", "")
	if len(stripped) < 7 {
		return match
	}
	return stripped[:4] + "****" + stripped[len(stripped)-3:]
}

func maskNIK(match string) string {
	if len(match) != 16 {
		return match
	}
	return match[:4] + "********" + match[12:]
}

// digitsOf returns only the ASCII digit characters of s.
func digitsOf(s string) string {
	digits := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, s[i])
		}
	}
	return string(digits)
}
