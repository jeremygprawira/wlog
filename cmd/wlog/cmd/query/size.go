// This file holds the size report of wlog query: the bytes one event costs by kind and
// operation, and the gigabytes per month at the rate of the sample.
package query

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/jeremygprawira/wlog/query"
)

// printSizes writes one row per kind and operation.
func printSizes(out io.Writer, events []map[string]any) error {
	writer := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "kind\toperation\tevents\tbytes\tbytes_per_event\tgb_per_month"); err != nil {
		return err
	}
	for _, row := range query.Sizes(events) {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%d\t%d\t%.1f\t%.3f\n",
			row.Kind, row.Operation, row.Events, row.Bytes, row.BytesPerEvent, row.GBPerMonth); err != nil {
			return err
		}
	}
	return writer.Flush()
}
