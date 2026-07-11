package model

import "fmt"

// unsupportedTaskType reports that a retrieval-only model was handed a task type it cannot
// express. Models that embed queries in a single (retrieval) mode return this when a caller
// passes a non-empty task type, rather than silently ignoring it and degrading the ranking.
func unsupportedTaskType(model string) error {
	return fmt.Errorf("model %q does not support a task type", model)
}
