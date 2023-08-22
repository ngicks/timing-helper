package timinghelper

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ngicks/mockable"
)

type pollParam struct {
	ctx          context.Context
	clockFactory func() mockable.Clock
}

func newPollParam() *pollParam {
	return &pollParam{
		ctx:          context.Background(),
		clockFactory: func() mockable.Clock { return mockable.NewClockReal() },
	}
}

type pollOption func(*pollParam)

func SetPollContext(ctx context.Context) pollOption {
	return func(pp *pollParam) {
		pp.ctx = ctx
	}
}

// Comment-out since it is not used.
//
// func setClockFactory(clockFactory func() mockable.Clock) pollOption {
// 	return func(pp *pollParam) {
// 		pp.clockFactory = clockFactory
// 	}
// }

// PollUntil calls predicate multiple times periodically at interval until it returns true,
// or timeout duration since the invocation of this function is passed.
func PollUntil(predicate func(ctx context.Context) bool, interval time.Duration, timeout time.Duration, options ...pollOption) (ok bool) {
	param := newPollParam()

	for _, opt := range options {
		opt(param)
	}

	ctx := param.ctx

	predCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	doneCh := make(chan struct{})

	defer func() {
		<-doneCh
	}()

	var called atomic.Bool
	done := func() {
		if called.CompareAndSwap(false, true) {
			cancel()
			close(doneCh)
		}
	}

	wait := make(chan struct{})
	defer func() {
		<-wait
	}()

	go func() {
		defer func() { close(wait) }()
		t := param.clockFactory()
		defer t.Stop()
		for {
			select {
			case <-doneCh:
				return
			default:
			}
			if predicate(predCtx) {
				break
			}
			t.Reset(interval) // t is known emitted or stopped.
			select {
			case <-t.C():
			case <-doneCh:
				return
			}
		}
		done()
	}()

	t := param.clockFactory()
	t.Reset(timeout)
	defer t.Stop()
	select {
	case <-ctx.Done():
		done()
		return false
	case <-t.C():
		done()
		return false
	default:
		select {
		case <-ctx.Done():
			done()
			return false
		case <-t.C():
			done()
			return false
		case <-doneCh:
			return true
		}
	}
}
