// This file holds the status class tables. An adapter maps its library's codes with
// them, so a 404 from an HTTP client, a NotFound from gRPC, and a usage exit code all
// count as the same client fault.
package work

import "strconv"

// StatusClass says whose fault a status is, which is what decides the level of an event.
type StatusClass int

const (
	StatusOK StatusClass = iota
	StatusClientError
	StatusServerError
)

// clientRPC names the gRPC codes that mean the caller's request was wrong.
var clientRPC = map[string]bool{
	"Canceled": true, "InvalidArgument": true, "NotFound": true, "AlreadyExists": true,
	"PermissionDenied": true, "FailedPrecondition": true, "OutOfRange": true,
	"Unauthenticated": true, "Aborted": true, "ResourceExhausted": true,
}

// ClassOf returns the status class of one code, for the kind that produced it.
//
// An adapter that has no class of its own reads it here. A code the table does not know is
// a server error, because an unknown failure is not the caller's fault.
func ClassOf(kind Kind, code string) StatusClass {
	switch kind {
	case KindRequest:
		if number, err := strconv.Atoi(code); err == nil {
			switch {
			case number >= 400 && number <= 499:
				return StatusClientError
			case number >= 500 && number <= 599:
				return StatusServerError
			}
		}
	case KindRPC:
		switch {
		case code == "" || code == "OK":
			return StatusOK
		case clientRPC[code]:
			return StatusClientError
		default:
			// A known server code and a code the table never saw are both the service's
			// fault, which is the safe reading of an unknown failure.
			return StatusServerError
		}
	case KindCommand:
		switch code {
		case "", "0":
			return StatusOK
		case "2":
			// Exit code 2 is the usage error of most command line tools.
			return StatusClientError
		default:
			return StatusServerError
		}
	}
	// A message, a job, and a function carry no code table: their error decides the level.
	return StatusOK
}
