// Command wlogvet runs the wlog map rules as a go vet tool:
//
//	go build -o $(go env GOPATH)/bin/wlogvet ./cmd/wlogvet
//	go vet -vettool=$(go env GOPATH)/bin/wlogvet ./...
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/jeremygprawira/wlog/cmd/wlog/analyzer"
)

func main() {
	singlechecker.Main(analyzer.Analyzer)
}
