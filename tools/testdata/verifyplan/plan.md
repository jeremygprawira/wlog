# A fixture plan

#### 1-OK-1 a task whose Verify command runs a test
**Acceptance:** this task passes.
**Verify:** `go test -race -run 'TestWorkspace_FindRoot' ./internal/workspace`.

#### 1-BAD-1 a task whose Verify command matches no test
**Acceptance:** this task fails.
**Verify:** `go test -race -run 'TestDoesNotExist_XYZ' ./internal/workspace`.

#### 1-PROSE-1 a task that describes a push instead of a command
**Acceptance:** the checker skips this one.
**Verify:** a branch push shows every required job green in `gh run view`.
