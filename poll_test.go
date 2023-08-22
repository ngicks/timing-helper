package timinghelper

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type pollPred struct {
	callCount      atomic.Int64
	blockOn        chan struct{}
	nextReturn     atomic.Bool
	onCtxCancelled func()
}

func newPollPred() *pollPred {
	p := &pollPred{
		blockOn: make(chan struct{}),
	}
	return p
}

func (p *pollPred) Pred(ctx context.Context) bool {
	p.callCount.Add(1)

	p.blockOn <- struct{}{}

	if p.onCtxCancelled != nil {
		<-ctx.Done()
		p.onCtxCancelled()
		return false
	}

	return p.nextReturn.Load()
}

func (p *pollPred) Unblock() {
	<-p.blockOn
}

func TestPollUntil(t *testing.T) {
	assert := assert.New(t)
	assert.True(PollUntil(func(ctx context.Context) bool { return true }, time.Hour, time.Hour))

	done := make(chan struct{})
	go func() {
		<-done
		pred := newPollPred()
		go func() {
			pred.Unblock()
		}()
		assert.False(
			PollUntil(pred.Pred, time.Hour, 100*time.Millisecond),
		)
		close(done)
	}()
	done <- struct{}{}

	dur := 500 * time.Millisecond
	select {
	case <-done:
	case <-time.After(dur):
		t.Errorf("Not timed-out: passed %s", dur)
	}

	for _, param := range []time.Duration{
		10 * time.Millisecond,
		30 * time.Millisecond,
		50 * time.Millisecond,
	} {
		pred := newPollPred()
		sw := make(chan struct{})
		go func() {
			<-sw
			pred.Unblock()
			old := time.Now()
			for {
				select {
				case <-pred.blockOn:
				case <-sw:
					return
				}
				now := time.Now()
				interval := now.Sub(old)
				assert.InDelta(param, interval, float64(5*time.Millisecond))
				old = now
			}
		}()
		sw <- struct{}{}
		PollUntil(pred.Pred, param, 100*time.Millisecond)
		close(sw)
		assert.InDelta(
			int64((100*time.Millisecond)/param),
			pred.callCount.Load(),
			1,
			"interval: %s", param,
		)
	}

	{
		pred := newPollPred()
		ctx, cancel := context.WithCancel(context.Background())
		go func() { cancel() }()
		done = make(chan struct{})
		go func() {
			PollUntil(pred.Pred, time.Hour, time.Hour, SetPollContext(ctx))
			close(done)
		}()
		go func() {
			// PollUntil blocked until predicate returns.
			pred.Unblock()
		}()

		dur = 500 * time.Millisecond
		select {
		case <-done:
		case <-time.After(dur):
			t.Errorf("Cancelling context is no-op: passed %s", dur)
		}
	}
	{
		pred := newPollPred()
		var called atomic.Bool
		pred.onCtxCancelled = func() {
			called.Store(true)
		}
		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			PollUntil(pred.Pred, time.Millisecond, time.Hour, SetPollContext(ctx))
			close(done)
		}()

		pred.Unblock()
		cancel()

		<-done

		assert.True(called.Load(), "cancelling the context passed to PollUntil"+
			" must also cancel the context passed to predicate")
	}
	{
		pred := newPollPred()
		var called atomic.Bool
		pred.onCtxCancelled = func() {
			called.Store(true)
		}

		done := make(chan struct{})
		go func() {
			PollUntil(pred.Pred, time.Millisecond, 100*time.Millisecond)
			close(done)
		}()

		pred.Unblock()
		<-done

		assert.True(called.Load(), "time-out of PollUntil"+
			" must cancel the context passed to predicate")
	}
}
