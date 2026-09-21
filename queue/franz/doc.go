// Package wlogfranz is wlog's franz-go adapter: one event per polled record and one call per
// produced record.
//
// Read top to bottom: Hooks returns one value for kgo.WithHooks. That value records a call
// for every produced record, adds the trace headers of the record context, and reads the
// trace of every fetched record into its context. Record starts one event for one polled
// record.
//
// This is the whole setup:
//
//	cl, _ := kgo.NewClient(kgo.SeedBrokers(brokers), kgo.ConsumerGroup("workers"),
//		kgo.WithHooks(wlogfranz.Hooks()))
//	for {
//		fetches := cl.PollFetches(ctx)
//		fetches.EachRecord(func(r *kgo.Record) {
//			rctx, end := wlogfranz.Record(log, cl, r)
//			err := handle(rctx, r)
//			end(err)
//			if err == nil {
//				cl.CommitRecords(rctx, r)
//			}
//		})
//	}
package wlogfranz
