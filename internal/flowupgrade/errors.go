package flowupgrade

import "fmt"

const (
	exitCodeSuccess         = 0
	exitCodeUsage           = 1
	exitCodeThreshold       = 2
	exitCodeSourceRead      = 3
	exitCodeRulePackInvalid = 4
	exitCodeVersionPair     = 5
	exitCodeInternal        = 10
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func newExitError(code int, format string, args ...any) *ExitError {
	return &ExitError{
		Code: code,
		Err:  fmt.Errorf(format, args...),
	}
}
