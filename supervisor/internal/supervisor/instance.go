package supervisor

import "errors"

// ErrAlreadyRunning is returned before touching service PID files or logs.
var ErrAlreadyRunning = errors.New("a supervisor already owns this run directory")
