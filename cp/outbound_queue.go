package cp

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

type outboundQueue struct {
	mu            sync.Mutex
	cond          *sync.Cond
	capacity      int
	callTimeout   time.Duration
	retryInFlight bool
	items         []*queuedOutboundCall
	conn          *dispatcher.Conn
	closed        bool
	closeErr      error
	done          chan struct{}
}

type queuedOutboundCall struct {
	ctx     context.Context
	cancel  context.CancelFunc
	stop    func() bool
	action  string
	payload []byte
	result  chan queuedOutboundResult
}

type queuedOutboundResult struct {
	payload []byte
	err     error
}

func newOutboundQueue(capacity int, callTimeout time.Duration, retryInFlight bool) *outboundQueue {
	q := &outboundQueue{
		capacity:      capacity,
		callTimeout:   callTimeout,
		retryInFlight: retryInFlight,
		done:          make(chan struct{}),
	}
	q.cond = sync.NewCond(&q.mu)
	go q.run()
	return q
}

func (q *outboundQueue) call(ctx context.Context, action string, payload []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, q.callTimeout)
	item := &queuedOutboundCall{
		ctx:     callCtx,
		cancel:  cancel,
		action:  action,
		payload: payload,
		result:  make(chan queuedOutboundResult, 1),
	}
	item.stop = context.AfterFunc(callCtx, func() {
		q.mu.Lock()
		q.cond.Broadcast()
		q.mu.Unlock()
	})

	q.mu.Lock()
	if q.closed {
		err := q.closeErr
		q.mu.Unlock()
		item.finish(nil, err)
		res := <-item.result
		return res.payload, res.err
	}
	if len(q.items) >= q.capacity {
		q.mu.Unlock()
		item.finish(nil, ocppj.ErrQueueFull)
		res := <-item.result
		return res.payload, res.err
	}
	q.items = append(q.items, item)
	q.cond.Signal()
	q.mu.Unlock()

	res := <-item.result
	return res.payload, res.err
}

func (q *outboundQueue) run() {
	defer close(q.done)
	for {
		item, conn, ok := q.next()
		if !ok {
			return
		}

		resp, err := dispatcher.DoCall(item.ctx, conn, item.action, item.payload)
		if closed, closeErr := q.closedState(); closed {
			q.finishHead(item, nil, closeErr)
			continue
		}
		if item.ctx.Err() != nil {
			q.finishHead(item, nil, queuedContextErr(item.ctx.Err()))
			continue
		}
		if err != nil && conn.Context().Err() != nil {
			// Connection dropped while this CALL was in flight. By default fail
			// it (OCPP gives no idempotency guarantee); only re-queue for resend
			// when the caller opted into WithRetryInFlightCalls.
			if q.retryInFlight {
				q.clearConn(conn)
				continue
			}
			q.finishHead(item, nil, ocppj.ErrConnClosed)
			q.clearConn(conn)
			continue
		}
		q.finishHead(item, resp, err)
	}
}

func (q *outboundQueue) next() (*queuedOutboundCall, *dispatcher.Conn, bool) {
	q.mu.Lock()
	for {
		if q.closed {
			items := q.items
			q.items = nil
			err := q.closeErr
			q.mu.Unlock()
			for _, item := range items {
				item.finish(nil, err)
			}
			return nil, nil, false
		}
		var canceled []*queuedOutboundCall
		var cancelErrs []error
		for i := 0; i < len(q.items); {
			if err := q.items[i].ctx.Err(); err != nil {
				canceled = append(canceled, q.items[i])
				cancelErrs = append(cancelErrs, queuedContextErr(err))
				q.items = append(q.items[:i], q.items[i+1:]...)
				continue
			}
			i++
		}
		if len(canceled) > 0 {
			q.mu.Unlock()
			for i, item := range canceled {
				item.finish(nil, cancelErrs[i])
			}
			q.mu.Lock()
			continue
		}
		if len(q.items) > 0 {
			item := q.items[0]
			if q.conn != nil && q.conn.Context().Err() == nil {
				conn := q.conn
				q.mu.Unlock()
				return item, conn, true
			}
		}
		q.cond.Wait()
	}
}

func (q *outboundQueue) finishHead(item *queuedOutboundCall, payload []byte, err error) {
	q.mu.Lock()
	if len(q.items) > 0 && q.items[0] == item {
		q.items = q.items[1:]
	} else {
		for i, queued := range q.items {
			if queued == item {
				q.items = append(q.items[:i], q.items[i+1:]...)
				break
			}
		}
	}
	q.cond.Broadcast()
	q.mu.Unlock()
	item.finish(payload, err)
}

func (q *outboundQueue) setConn(conn *dispatcher.Conn) {
	q.mu.Lock()
	q.conn = conn
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *outboundQueue) clearConn(conn *dispatcher.Conn) {
	q.mu.Lock()
	if q.conn == conn {
		q.conn = nil
	}
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *outboundQueue) close(err error) {
	if err == nil {
		err = ocppj.ErrConnClosed
	}
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		q.closeErr = err
		q.cond.Broadcast()
	}
	q.mu.Unlock()
	<-q.done
}

func (q *outboundQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *outboundQueue) closedState() (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed, q.closeErr
}

func (item *queuedOutboundCall) finish(payload []byte, err error) {
	if item.stop != nil {
		item.stop()
	}
	if item.cancel != nil {
		item.cancel()
	}
	item.result <- queuedOutboundResult{payload: payload, err: err}
}

func queuedContextErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ocppj.ErrCallTimeout
	}
	return err
}
