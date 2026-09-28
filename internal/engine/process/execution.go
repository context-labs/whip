package process

import "context"

// Cell supplies execution input independently of any model or tool dispatcher.
type Cell struct {
	Code     string
	CallID   string
	OnOutput func(string)
}

// HostObserver observes one invocation and optionally its eventual completion.
// The returned callback is called once, including when dispatch is cancelled.
type HostObserver func(HostCall, map[string]any) func(HostCall, any)

type hostOperationReporterKey struct{}

func withHostOperationReporter(ctx context.Context, report func(string)) context.Context {
	return context.WithValue(ctx, hostOperationReporterKey{}, report)
}

// ReportHostOperation records the durable operation admitted by the current
// Host.Call. Call it synchronously before that invocation returns.
func ReportHostOperation(ctx context.Context, id string) {
	if report, ok := ctx.Value(hostOperationReporterKey{}).(func(string)); ok {
		report(id)
	}
}
