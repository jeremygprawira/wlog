package mcpfixture

import _ "embed"

//go:embed tool_call.json
var toolCall string

//go:embed tool_error.json
var toolError string

//go:embed unknown_tool.json
var unknownTool string

//go:embed input_required.json
var inputRequired string
