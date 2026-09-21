// This file holds the fake client of the tests. A test cannot reach a Redis server, so the
// fake records the tasks that Enqueue sends.
package wlogasynq

import (
	"context"
	"sync"

	"github.com/hibiken/asynq"
)

// fakeClient records the tasks of every enqueue, and reports err when the test set one.
type fakeClient struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	err   error
}

// EnqueueContext records one task, and returns err when the test set one.
func (c *fakeClient) EnqueueContext(_ context.Context, task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	c.tasks = append(c.tasks, task)
	return &asynq.TaskInfo{ID: "task-1"}, nil
}

// last returns the last enqueued task, and nil when the client enqueued none.
func (c *fakeClient) last() *asynq.Task {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.tasks) == 0 {
		return nil
	}
	return c.tasks[len(c.tasks)-1]
}
