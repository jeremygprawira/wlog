// Package wlogcron is wlog's robfig/cron adapter: one event per job run.
//
// Read top to bottom: Wrap returns the job wrapper that opens one event per run, and Job
// returns a cron job from one function with a context and an error.
//
// Place Wrap inside cron.SkipIfStillRunning, so a skipped run records nothing. In a
// short-lived process, call Stop and wait for the context it returns, then flush the Logger.
package wlogcron
