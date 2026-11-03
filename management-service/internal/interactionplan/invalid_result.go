package interactionplan

import "errors"

// ErrInvalidResult marks a returned item which cannot be used. Retrying the
// same paid request cannot repair its already returned content.
var ErrInvalidResult = errors.New("互动结果无法确认，丢弃本条")

type invalidResultError struct{ error }

func (e invalidResultError) Is(target error) bool { return target == ErrInvalidResult }
func (e invalidResultError) Unwrap() error        { return e.error }
