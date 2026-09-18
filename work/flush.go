// This file holds the flush option of Run, for a runtime that ends with the process.
package work

// Flush makes Run call log.Flush before it returns, so a short lived runtime, such as a
// command line tool or a serverless function, does not exit with events still queued.
//
// A long lived service does not need it, because the writer of the Logger drains its own
// queue while the process runs.
func Flush() RunOption { return func(c *runConfig) { c.flush = true } }
