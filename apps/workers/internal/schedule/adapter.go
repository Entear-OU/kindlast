package schedule

import (
	"log/slog"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/log"
)

// activityOptions pins an activity's registered name, so the name the
// workflow executes and the name the worker registers are one constant
// rather than two spellings.
func activityOptions(name string) activity.RegisterOptions {
	return activity.RegisterOptions{Name: name}
}

// slogAdapter lets the SDK log through the same structured logger the rest of
// this binary uses, so an engine warning lands in the same stream and the same
// shape as a gateway warning. The SDK's interface is key-value pairs, which is
// slog's too.
type slogAdapter struct{ l *slog.Logger }

var _ log.Logger = slogAdapter{}

func (a slogAdapter) Debug(msg string, keyvals ...any) { a.logger().Debug(msg, keyvals...) }
func (a slogAdapter) Info(msg string, keyvals ...any)  { a.logger().Info(msg, keyvals...) }
func (a slogAdapter) Warn(msg string, keyvals ...any)  { a.logger().Warn(msg, keyvals...) }
func (a slogAdapter) Error(msg string, keyvals ...any) { a.logger().Error(msg, keyvals...) }

func (a slogAdapter) logger() *slog.Logger {
	if a.l == nil {
		return slog.Default()
	}
	return a.l.With("component", "temporal")
}
