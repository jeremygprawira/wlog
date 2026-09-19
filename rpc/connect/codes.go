// This file holds the code names. The work table spells a code the gRPC way, and Connect
// spells the same code with underscores, so the adapter maps one to the other.
package wlogconnect

import "connectrpc.com/connect"

// codeOK is the code of a call that finished without an error. Connect v1.18.1 exports no
// constant for it, because the zero value is OK.
const codeOK connect.Code = 0

// codeNames maps a Connect code to the name of the work status table.
var codeNames = map[connect.Code]string{
	codeOK:                         "OK",
	connect.CodeCanceled:           "Canceled",
	connect.CodeUnknown:            "Unknown",
	connect.CodeInvalidArgument:    "InvalidArgument",
	connect.CodeDeadlineExceeded:   "DeadlineExceeded",
	connect.CodeNotFound:           "NotFound",
	connect.CodeAlreadyExists:      "AlreadyExists",
	connect.CodePermissionDenied:   "PermissionDenied",
	connect.CodeResourceExhausted:  "ResourceExhausted",
	connect.CodeFailedPrecondition: "FailedPrecondition",
	connect.CodeAborted:            "Aborted",
	connect.CodeOutOfRange:         "OutOfRange",
	connect.CodeUnimplemented:      "Unimplemented",
	connect.CodeInternal:           "Internal",
	connect.CodeUnavailable:        "Unavailable",
	connect.CodeDataLoss:           "DataLoss",
	connect.CodeUnauthenticated:    "Unauthenticated",
}

// codeName returns the work table name of one code. A code the table never saw keeps its
// wire name, which the table reads as the service's fault.
func codeName(code connect.Code) string {
	if name, ok := codeNames[code]; ok {
		return name
	}
	return code.String()
}
