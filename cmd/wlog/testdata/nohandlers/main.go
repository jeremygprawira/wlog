// Command nohandlers is a fixture for the wlog map zero-handler test: it serves no route that
// the analyzer can see, so a run must fail rather than report a perfect score.
package main

import "fmt"

func main() {
	fmt.Println("no handlers here")
}
