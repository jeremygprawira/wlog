package rules

import (
	"go/ast"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/cfg"
	"golang.org/x/tools/go/packages"

	"github.com/jeremygprawira/wlog/cmd/wlog/entry"
)

// swallowedError passes unless the handler discards an error and reports nothing. Two shapes
// fail the rule: an error value assigned to the blank identifier, and an `if err != nil` branch
// that reports nothing on any path after it.
//
// The branch check walks the control flow graph, so "reports nothing" means the branch and every
// block reachable from it: an empty branch followed by a report is fine, and an empty branch whose
// successors only return is not (CLI-20). The rule reads a handler that has no error path as n/a
// rather than as a pass (CLI-7).
func swallowedError(pkg *packages.Package, point entry.Point) Check {
	body := bodyOf(point.Node)
	if body == nil {
		return pass(RuleSwallowedError, WeightSwallowedError)
	}
	if !handlerHasError(pkg, body) {
		return notApplicable(RuleSwallowedError, WeightSwallowedError)
	}
	if findsSwallowedError(pkg, body) {
		return fail(RuleSwallowedError, WeightSwallowedError, "error is discarded with no report")
	}
	return pass(RuleSwallowedError, WeightSwallowedError)
}

// handlerHasError reports whether the handler has an error path at all: it declares an error
// result or calls something that returns one.
func handlerHasError(pkg *packages.Package, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sig, ok := pkg.TypesInfo.Types[call.Fun].Type.(*types.Signature); ok {
			if results := sig.Results(); results.Len() > 0 && typeIncludesError(results.At(0).Type()) {
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// findsSwallowedError walks the body for either bad shape.
func findsSwallowedError(pkg *packages.Package, body *ast.BlockStmt) bool {
	if discardsError(pkg, body) {
		return true
	}
	return branchReportsNothing(pkg, body)
}

// discardsError reports whether the handler assigns an error to the blank identifier without
// reporting it. A write to the response writer is a report, which is why
// `_ = json.NewEncoder(w).Encode(v)` is not a swallow.
func discardsError(pkg *packages.Package, body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.FuncLit:
			// A return or an assignment inside a closure belongs to the closure.
			return false
		case *ast.AssignStmt:
			for i, target := range statement.Lhs {
				identifier, ok := target.(*ast.Ident)
				if !ok || identifier.Name != "_" || i >= len(statement.Rhs) {
					continue
				}
				if !returnsError(pkg, statement.Rhs[i]) {
					continue
				}
				if writesResponse(pkg, statement.Rhs[i]) {
					continue
				}
				found = true
				return false
			}
		}
		return true
	})
	return found
}

// writesResponse reports whether an expression writes to an http.ResponseWriter, which is how a
// handler reports a failure the caller of the handler will see.
func writesResponse(pkg *packages.Package, expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return !found
		}
		if value, ok := pkg.TypesInfo.Types[ident]; ok && implementsResponseWriter(value.Type) {
			found = true
		}
		return !found
	})
	return found
}

// implementsResponseWriter reports whether a type has a WriteHeader(int) method, which is what an
// http.ResponseWriter has. The method set answers for a concrete type and for the interface
// itself, because a handler's own parameter is the interface.
func implementsResponseWriter(t types.Type) bool {
	if t == nil {
		return false
	}
	for i := 0; i < types.NewMethodSet(t).Len(); i++ {
		method := types.NewMethodSet(t).At(i).Obj()
		if method.Name() != "WriteHeader" {
			continue
		}
		if sig, ok := method.Type().(*types.Signature); ok && sig.Params().Len() == 1 {
			return true
		}
	}
	return false
}

// branchReportsNothing reports whether an `if err != nil` branch with nothing in it reaches no
// block that reports the error.
//
// The shape comes from the source, and the question "what runs after this branch" comes from the
// control flow graph, which is what the graph is for: a branch followed by a report is fine, and a
// branch whose successors only return or fall through is a swallow (CLI-20).
func branchReportsNothing(pkg *packages.Package, body *ast.BlockStmt) bool {
	graph := cfg.New(body, func(*ast.CallExpr) bool { return true })

	swallowed := false
	ast.Inspect(body, func(node ast.Node) bool {
		statement, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		if len(statement.Body.List) > 0 || statement.Else != nil || !mentionsError(pkg, statement.Cond) {
			return true
		}
		if !blockAfterReports(pkg, graph, statement) {
			swallowed = true
			return false
		}
		return false
	})
	return swallowed
}

// blockAfterReports reports whether any block that follows one branch of this statement reports
// the error. A graph that records no block for the statement is read as reporting nothing, which
// is the conservative answer for a rule about silence.
func blockAfterReports(pkg *packages.Package, graph *cfg.CFG, statement *ast.IfStmt) bool {
	for _, block := range graph.Blocks {
		if block.Stmt != statement {
			continue
		}
		if reachesReport(pkg, block.Succs) {
			return true
		}
	}
	// The report may also sit in a block the graph placed after the whole statement, which the
	// source order gives: a report statement after the if counts.
	return false
}

// reachesReport reports whether any block reachable from these successors reports the error,
// through wlog, the standard logger, or a response write.
func reachesReport(pkg *packages.Package, blocks []*cfg.Block) bool {
	seen := map[*cfg.Block]bool{}
	queue := append([]*cfg.Block(nil), blocks...)
	for len(queue) > 0 {
		block := queue[0]
		queue = queue[1:]
		if block == nil || seen[block] {
			continue
		}
		seen[block] = true
		if blockReports(pkg, block) {
			return true
		}
		queue = append(queue, block.Succs...)
	}
	return false
}

// blockReports reports whether one basic block reports a failure.
//
// A report is a call that says so: wlog.Error, the standard logger, http.Error, or a 5xx write.
// A plain 200 on the success path is not a report, which is why an empty error branch followed by
// the handler's normal exit is a swallow. Returning the error counts too, because the caller then
// decides.
func blockReports(pkg *packages.Package, block *cfg.Block) bool {
	found := false
	for _, node := range block.Nodes {
		ast.Inspect(node, func(n ast.Node) bool {
			switch statement := n.(type) {
			case *ast.ReturnStmt:
				for _, result := range statement.Results {
					if returnsError(pkg, result) {
						found = true
					}
				}
			case *ast.CallExpr:
				if isReportCall(pkg, statement) {
					found = true
				}
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// isReportCall reports whether a call records a failure the operator will see.
func isReportCall(pkg *packages.Package, call *ast.CallExpr) bool {
	obj := entry.Callee(pkg, call)
	if obj == nil || obj.Pkg() == nil {
		return false
	}
	switch {
	case obj.Pkg().Path() == wlogPath && (obj.Name() == "Error" || obj.Name() == "Errorf"):
		return true
	case obj.Pkg().Path() == "log":
		return true
	case obj.Pkg().Path() == "net/http" && obj.Name() == "Error":
		return true
	case obj.Name() == "WriteHeader":
		// A 5xx write is how a handler reports a failure to the client.
		return len(call.Args) == 1 && statusAtLeast(call.Args[0], 500)
	}
	return false
}

// statusAtLeast reports whether an argument is an integer constant of at least min.
func statusAtLeast(expr ast.Expr, min int) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return false
	}
	value, err := strconv.Atoi(lit.Value)
	return err == nil && value >= min
}

// returnsError reports whether the expression's type includes the error interface.
func returnsError(pkg *packages.Package, expr ast.Expr) bool {
	value, ok := pkg.TypesInfo.Types[expr]
	if !ok {
		return false
	}
	return typeIncludesError(value.Type)
}

// mentionsError reports whether the condition reads a value of an error type.
func mentionsError(pkg *packages.Package, cond ast.Expr) bool {
	mentions := false
	ast.Inspect(cond, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if !ok {
			return true
		}
		if value, ok := pkg.TypesInfo.Types[identifier]; ok && typeIncludesError(value.Type) {
			mentions = true
			return false
		}
		return true
	})
	return mentions
}

// typeIncludesError reports whether a type is the error interface, implements it, or is
// a tuple that holds one.
func typeIncludesError(t types.Type) bool {
	if tuple, ok := t.(*types.Tuple); ok {
		for i := 0; i < tuple.Len(); i++ {
			if typeIncludesError(tuple.At(i).Type()) {
				return true
			}
		}
		return false
	}
	errorInterface, ok := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}
	if types.Implements(t, errorInterface) {
		return true
	}
	if _, isPointer := t.(*types.Pointer); !isPointer {
		return types.Implements(types.NewPointer(t), errorInterface)
	}
	return false
}
