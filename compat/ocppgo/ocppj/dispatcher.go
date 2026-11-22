package ocppj

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// ClientDispatcher contains the state and logic for handling outgoing messages on a client endpoint.
// This allows the ocpp-j layer to delegate queueing and processing logic to an external entity.
//
// The dispatcher writes outgoing messages directly to the networking layer, using a previously set websocket client.
//
// A ClientState needs to be passed to the dispatcher, before starting it.
// The dispatcher is in charge of managing pending requests while handling the request flow.
type ClientDispatcher interface {
	// Starts the dispatcher. Depending on the implementation, this may
	// start a dedicated goroutine or simply allocate the necessary state.
	Start()
	// Sets the maximum timeout to be considered after sending a request.
	// If a response to the request is not received within the specified period, the request
	// is discarded and an error is returned to the caller.
	//
	// The timeout is reset upon a disconnection/reconnection.
	//
	// This function must be called before starting the dispatcher, otherwise it may lead to unexpected behavior.
	SetTimeout(timeout time.Duration)
	// Returns true, if the dispatcher is currently running, false otherwise.
	// If the dispatcher is paused, the function still returns true.
	IsRunning() bool
	// Returns true, if the dispatcher is currently paused, false otherwise.
	// If the dispatcher is not running at all, the function will still return false.
	IsPaused() bool
	// Dispatches a request. Depending on the implementation, this may first queue a request
	// and process it later, asynchronously, or write it directly to the networking layer.
	//
	// If no network client was set, or the request couldn't be processed, an error is returned.
	SendRequest(req RequestBundle) error
	// Notifies the dispatcher that a request has been completed (i.e. a response was received).
	// The dispatcher takes care of removing the request marked by the requestID from
	// the pending requests. It will then attempt to process the next queued request.
	CompleteRequest(requestID string)
	// Sets a callback to be invoked when a request gets canceled, due to network timeouts or internal errors.
	// The callback passes the original message ID and request struct of the failed request, along with an error.
	//
	// Calling Stop on the dispatcher will not trigger this callback.
	//
	// If no callback is set, a request will still be removed from the dispatcher when a timeout occurs.
	SetOnRequestCanceled(cb func(requestID string, request ocpp.Request, err *ocpp.Error))
	// Sets the network client, so the dispatcher may send requests using the networking layer directly.
	//
	// This needs to be set before calling the Start method. If not, sending requests will fail.
	SetNetworkClient(client ws.WsClient)
	// Sets the state manager for pending requests in the dispatcher.
	//
	// The state should only be accessed by the dispatcher while running.
	SetPendingRequestState(stateHandler ClientState)
	// Stops a running dispatcher. This will clear all state and empty the internal queues.
	//
	// If an onRequestCanceled callback is set, it won't be triggered by stopping the dispatcher.
	Stop()
	// Notifies that an external event (typically network-related) should pause
	// the dispatcher. Internal timers will be stopped an no further requests
	// will be set to pending. You may keep enqueuing requests.
	// Use the Resume method for re-starting the dispatcher.
	Pause()
	// Undoes a previous pause operation, restarting internal timers and the
	// regular request flow.
	//
	// If there was a pending request before pausing the dispatcher, a response/timeout
	// for this request shall be awaited anew.
	Resume()
}

// pendingRequest is used internally for associating metadata to a pending Request.
type pendingRequest struct {
	request ocpp.Request
}

// DefaultClientDispatcher is a default implementation of the ClientDispatcher interface.
//
// The dispatcher implements the ClientState as well for simplicity.
// Access to pending requests is thread-safe.
type DefaultClientDispatcher struct {
	requestQueue        RequestQueue
	requestChannel      chan bool
	stopped             bool
	readyForDispatch    chan bool
	pendingRequestState ClientState
	network             ws.WsClient
	mutex               sync.RWMutex
	onRequestCancel     func(requestID string, request ocpp.Request, err *ocpp.Error)
	timer               *time.Timer
	paused              bool
	timeout             time.Duration
}

const defaultTimeoutTick = 24 * time.Hour
const defaultMessageTimeout = 30 * time.Second

// NewDefaultClientDispatcher creates a new DefaultClientDispatcher struct.
func NewDefaultClientDispatcher(queue RequestQueue) *DefaultClientDispatcher {
	return &DefaultClientDispatcher{
		requestQueue:        queue,
		requestChannel:      nil,
		readyForDispatch:    make(chan bool, 1),
		pendingRequestState: NewClientState(),
		timeout:             defaultMessageTimeout,
	}
}

func (d *DefaultClientDispatcher) SetOnRequestCanceled(cb func(requestID string, request ocpp.Request, err *ocpp.Error)) {
	d.onRequestCancel = cb
}

func (d *DefaultClientDispatcher) SetTimeout(timeout time.Duration) {
	d.timeout = timeout
}

func (d *DefaultClientDispatcher) Start() {
	d.mutex.Lock()
	d.requestChannel = make(chan bool, 1)
	d.stopped = false
	d.mutex.Unlock()
	d.timer = time.NewTimer(defaultTimeoutTick) // Default to 24 hours tick
	go d.messagePump()
}

func (d *DefaultClientDispatcher) IsRunning() bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.requestChannel != nil
}

func (d *DefaultClientDispatcher) IsPaused() bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	return d.paused
}

func (d *DefaultClientDispatcher) Stop() {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.requestChannel == nil {
		return
	}
	close(d.requestChannel)
	// The channel stays non-nil until the pump notices the close, so IsRunning
	// keeps answering true for that window; sends are guarded by stopped instead.
	d.stopped = true
	// TODO: clear pending requests?
}

func (d *DefaultClientDispatcher) SetNetworkClient(client ws.WsClient) {
	d.network = client
}

func (d *DefaultClientDispatcher) SetPendingRequestState(state ClientState) {
	d.pendingRequestState = state
}

// SendRequest queues a request for delivery.
//
// A disconnect can stop the dispatcher while a caller — a heartbeat loop, a
// meter-value sender — is right here, and sending on the closed channel would
// take the process down. The stopped flag is checked under the same lock Stop
// holds, so such a caller gets an error back instead.
func (d *DefaultClientDispatcher) SendRequest(req RequestBundle) error {
	if d.network == nil {
		return fmt.Errorf("cannot SendRequest, no network client was set")
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.stopped || d.requestChannel == nil {
		return fmt.Errorf("cannot SendRequest, dispatcher is stopped")
	}
	if err := d.requestQueue.Push(req); err != nil {
		return err
	}
	// Non-blocking wake: the pump checks the queue on every wake, so when the
	// buffer already holds a signal this push is covered by it. Blocking here
	// while holding the lock would deadlock against the pump, which takes the
	// same lock in IsPaused.
	select {
	case d.requestChannel <- true:
	default:
	}
	return nil
}

func (d *DefaultClientDispatcher) messagePump() {
	rdy := true // Ready to transmit at the beginning
	for {
		select {
		case _, ok := <-d.requestChannel:
			// New request was posted
			if !ok {
				d.requestQueue.Init()
				d.requestChannel = nil
				return
			}
		case _, ok := <-d.timer.C:
			// Timeout elapsed
			if !ok {
				continue
			}
			if d.pendingRequestState.HasPendingRequest() {
				// Current request timed out. Removing request and triggering cancel callback
				el := d.requestQueue.Peek()
				bundle, _ := el.(RequestBundle)
				d.CompleteRequest(bundle.Call.UniqueId)
				if d.onRequestCancel != nil {
					d.onRequestCancel(bundle.Call.UniqueId, bundle.Call.Payload,
						ocpp.NewError(GenericError, "Request timed out", bundle.Call.UniqueId))
				}
			}
			// No request is currently pending -> set timer to high number
			d.timer.Reset(defaultTimeoutTick)
		case rdy = <-d.readyForDispatch:
			// Ready flag set, keep going
		}
		// Check if dispatcher is paused
		d.mutex.Lock()
		paused := d.paused
		d.mutex.Unlock()
		if paused {
			// Ignore dispatch events as long as dispatcher is paused
			continue
		}
		// Only dispatch request if able to send and request queue isn't empty
		if rdy && !d.requestQueue.IsEmpty() {
			d.dispatchNextRequest()
			rdy = false
			// Set timer
			if !d.timer.Stop() {
				<-d.timer.C
			}
			d.timer.Reset(d.timeout)
		}
	}
}

func (d *DefaultClientDispatcher) dispatchNextRequest() {
	// Get first element in queue
	el := d.requestQueue.Peek()
	bundle, _ := el.(RequestBundle)
	jsonMessage := bundle.Data
	d.pendingRequestState.AddPendingRequest(bundle.Call.UniqueId, bundle.Call.Payload)
	// Attempt to send over network
	err := d.network.Write(jsonMessage)
	if err != nil {
		//TODO: handle retransmission instead of skipping request altogether
		d.CompleteRequest(bundle.Call.GetUniqueId())
		if d.onRequestCancel != nil {
			d.onRequestCancel(bundle.Call.UniqueId, bundle.Call.Payload,
				ocpp.NewError(InternalError, err.Error(), bundle.Call.UniqueId))
		}
	}
	log.Infof("dispatched request %s to server", bundle.Call.UniqueId)
	log.Debugf("sent JSON message to server: %s", string(jsonMessage))
}

func (d *DefaultClientDispatcher) Pause() {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if !d.timer.Stop() {
		<-d.timer.C
	}
	d.timer.Reset(defaultTimeoutTick)
	d.paused = true
}

func (d *DefaultClientDispatcher) Resume() {
	d.mutex.Lock()
	d.paused = false
	d.mutex.Unlock()
	if d.pendingRequestState.HasPendingRequest() {
		// There is a pending request already. Awaiting response, before dispatching new requests.
		d.timer.Reset(d.timeout)
	} else {
		// Can dispatch a new request. Notifying message pump.
		d.signalReadyForDispatch()
	}
}

func (d *DefaultClientDispatcher) CompleteRequest(requestId string) {
	el := d.requestQueue.Peek()
	if el == nil {
		log.Errorf("attempting to pop front of queue, but queue is empty")
		return
	}
	bundle, _ := el.(RequestBundle)
	if bundle.Call.UniqueId != requestId {
		log.Errorf("internal state mismatch: received response for %v but expected response for %v", requestId, bundle.Call.UniqueId)
		return
	}
	d.requestQueue.Pop()
	d.pendingRequestState.DeletePendingRequest(requestId)
	log.Debugf("removed request %v from front of queue", bundle.Call.UniqueId)
	// Signal that next message in queue may be sent
	d.signalReadyForDispatch()
}

// signalReadyForDispatch tells messagePump that the next request may be sent.
//
// The flag carries no information beyond "true", so when the buffer already holds one
// this signal is covered by it and nothing needs to wait. A blocking send would wedge
// the pump against its own signal when it completes a timed-out or failed request, and
// would leave a caller blocked for good once the pump has stopped.
func (d *DefaultClientDispatcher) signalReadyForDispatch() {
	select {
	case d.readyForDispatch <- true:
	default:
	}
}

// ServerDispatcher contains the state and logic for handling outgoing messages on a server endpoint.
// This allows the ocpp-j layer to delegate queueing and processing logic to an external entity.
//
// The dispatcher writes outgoing messages directly to the networking layer, using a previously set websocket server.
//
// A ClientState needs to be passed to the dispatcher, before starting it.
// The dispatcher is in charge of managing all pending requests to clients, while handling the request flow.
type ServerDispatcher interface {
	// Starts the dispatcher. Depending on the implementation, this may
	// start a dedicated goroutine or simply allocate the necessary state.
	Start()
	// Returns true, if the dispatcher is currently running, false otherwise.
	// If the dispatcher is paused, the function still returns true.
	IsRunning() bool
	// Sets the maximum timeout to be considered after sending a request.
	// If a response to the request is not received within the specified period, the request
	// is discarded and an error is returned to the caller.
	//
	// One timeout per client runs in the background.
	// The timeout is reset whenever a response comes in, the connection is closed, or the server is stopped.
	//
	// This function must be called before starting the dispatcher, otherwise it may lead to unexpected behavior.
	SetTimeout(timeout time.Duration)
	// Dispatches a request for a specific client. Depending on the implementation, this may first queue
	// a request and process it later (asynchronously), or write it directly to the networking layer.
	//
	// If no network server was set, or the request couldn't be processed, an error is returned.
	SendRequest(clientID string, req RequestBundle) error
	// Notifies the dispatcher that a request has been completed (i.e. a response was received),
	// for a specific client.
	// The dispatcher takes care of removing the request marked by the requestID from
	// that client's pending requests. It will then attempt to process the next queued request.
	CompleteRequest(clientID string, requestID string)
	// Sets a callback to be invoked when a request gets canceled, due to network timeouts.
	// The callback passes the original client ID, message ID, and request struct of the failed request,
	// along with an error.
	//
	// Calling Stop on the dispatcher will not trigger this callback.
	//
	// If no callback is set, a request will still be removed from the dispatcher when a timeout occurs.
	SetOnRequestCanceled(cb CanceledRequestHandler)
	// Sets the network server, so the dispatcher may send requests using the networking layer directly.
	//
	// This needs to be set before calling the Start method. If not, sending requests will fail.
	SetNetworkServer(server ws.WsServer)
	// Sets the state manager for pending requests in the dispatcher.
	//
	// The state should only be accessed by the dispatcher while running.
	SetPendingRequestState(stateHandler ServerState)
	// Stops a running dispatcher. This will clear all state and empty the internal queues.
	//
	// If an onRequestCanceled callback is set, it won't be triggered by stopping the dispatcher.
	Stop()
	// Notifies that it is now possible to dispatch requests for a new client.
	//
	// Internal queues are created and requests for the client are now accepted.
	CreateClient(clientID string)
	// Notifies that a client was invalidated (typically caused by a network event).
	//
	// The dispatcher will stop dispatching requests for that specific client.
	// Internal queues for that client are cleared and no further requests will be accepted.
	// Undelivered pending requests are also cleared.
	// The OnRequestCanceled callback will be invoked for each discarded request.
	DeleteClient(clientID string)
}

// DefaultServerDispatcher is a default implementation of the ServerDispatcher interface.
//
// The dispatcher implements the ClientState as well for simplicity.
// Access to pending requests is thread-safe.
type DefaultServerDispatcher struct {
	queueMap            ServerQueueMap
	requestChannel      chan string
	deletedChannel      chan string
	readyForDispatch    chan string
	pendingRequestState ServerState
	timeout             time.Duration
	timerC              chan string
	running             bool
	stoppedC            chan struct{}
	onRequestCancel     CanceledRequestHandler
	network             ws.WsServer
	mutex               sync.RWMutex
}

// Handler function to be invoked when a request gets canceled (either due to timeout or to other external factors).
type CanceledRequestHandler func(clientID string, requestID string, request ocpp.Request, err *ocpp.Error)

// Utility struct for passing a client context around and cancel pending requests.
type clientTimeoutContext struct {
	ctx    context.Context
	cancel func()
}

func (c clientTimeoutContext) isActive() bool {
	return c.cancel != nil
}

// NewDefaultServerDispatcher creates a new DefaultServerDispatcher struct.
func NewDefaultServerDispatcher(queueMap ServerQueueMap) *DefaultServerDispatcher {
	d := &DefaultServerDispatcher{
		queueMap:         queueMap,
		requestChannel:   nil,
		readyForDispatch: make(chan string, 1),
		timeout:          defaultMessageTimeout,
	}
	d.pendingRequestState = NewServerState(&d.mutex)
	return d
}

func (d *DefaultServerDispatcher) Start() {
	// The channels are re-created on every start, so the pump and the timeout goroutines
	// of this run get their own copies and never look at the next run's fields.
	d.mutex.Lock()
	requestC := make(chan string, 20)
	deletedC := make(chan string, 20)
	timerC := make(chan string, 10)
	stoppedC := make(chan struct{}, 1)
	d.requestChannel = requestC
	d.deletedChannel = deletedC
	d.timerC = timerC
	d.stoppedC = stoppedC
	d.running = true
	d.mutex.Unlock()
	go d.messagePump(requestC, deletedC, timerC, stoppedC)
}

func (d *DefaultServerDispatcher) IsRunning() bool {
	d.mutex.RLock()
	defer d.mutex.RUnlock()
	return d.running
}

func (d *DefaultServerDispatcher) Stop() {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if !d.running {
		return
	}
	d.running = false
	close(d.stoppedC)
}

func (d *DefaultServerDispatcher) SetTimeout(timeout time.Duration) {
	d.timeout = timeout
}

func (d *DefaultServerDispatcher) CreateClient(clientID string) {
	if d.IsRunning() {
		_ = d.queueMap.GetOrCreate(clientID)
	}
}

func (d *DefaultServerDispatcher) DeleteClient(clientID string) {
	d.queueMap.Remove(clientID)
	d.mutex.RLock()
	running, deletedC, stoppedC := d.running, d.deletedChannel, d.stoppedC
	d.mutex.RUnlock()
	if running {
		// Announced on its own channel: by the time the pump gets here the same client
		// ID may have been created again by a reconnecting charge point, and a deletion
		// is then indistinguishable from a new request on the shared request channel.
		signalClient(deletedC, stoppedC, clientID)
	}
}

func (d *DefaultServerDispatcher) SetNetworkServer(server ws.WsServer) {
	d.network = server
}

func (d *DefaultServerDispatcher) SetOnRequestCanceled(cb CanceledRequestHandler) {
	d.onRequestCancel = cb
}

func (d *DefaultServerDispatcher) SetPendingRequestState(state ServerState) {
	d.pendingRequestState = state
}

func (d *DefaultServerDispatcher) SendRequest(clientID string, req RequestBundle) error {
	if d.network == nil {
		return fmt.Errorf("cannot send request %v, no network server was set", req.Call.UniqueId)
	}
	d.mutex.RLock()
	running, requestC, stoppedC := d.running, d.requestChannel, d.stoppedC
	d.mutex.RUnlock()
	if !running {
		return fmt.Errorf("cannot send request %s, dispatcher is not running", req.Call.UniqueId)
	}
	q, ok := d.queueMap.Get(clientID)
	if !ok {
		return fmt.Errorf("cannot send request %s, no client %s exists", req.Call.UniqueId, clientID)
	}
	if err := q.Push(req); err != nil {
		return err
	}
	// The pump may run a request-canceled callback that sends a request itself, so this
	// must not wait for the pump.
	signalClient(requestC, stoppedC, clientID)
	return nil
}

// signalClient tells messagePump about clientID on ch without ever blocking the caller.
//
// messagePump is the only reader of these channels, and it runs user callbacks
// (request canceled) and takes the dispatcher's lock, so a caller waiting for room in
// the buffer can wedge it against its own signal. When the buffer is busy, a sender
// goroutine waits on the pump's behalf instead and gives up once the dispatcher stops.
func signalClient(ch chan string, stoppedC chan struct{}, clientID string) {
	select {
	case ch <- clientID:
	default:
		go func() {
			select {
			case ch <- clientID:
			case <-stoppedC:
			}
		}()
	}
}

// requestPump processes new outgoing requests for each client and makes sure they are processed sequentially.
// This method is executed by a dedicated coroutine as soon as the server is started and runs indefinitely.
func (d *DefaultServerDispatcher) messagePump(requestC, deletedC, timerC chan string, stoppedC chan struct{}) {
	var clientID string
	var ok bool
	var rdy bool
	var clientCtx clientTimeoutContext
	var clientQueue RequestQueue
	clientContextMap := map[string]clientTimeoutContext{} // Empty at the beginning
	// Dispatcher Loop
	for {
		select {
		case <-stoppedC:
			// Server was stopped
			d.queueMap.Init()
			log.Info("stopped processing requests")
			return
		case clientID = <-requestC:
			// Check whether there is a request queue for the specified client
			clientQueue, ok = d.queueMap.Get(clientID)
			if !ok {
				// No client queue found (client was removed)
				// Deleting and canceling the context
				clientCtx = clientContextMap[clientID]
				delete(clientContextMap, clientID)
				if clientCtx.ctx != nil {
					clientCtx.cancel()
				}
				continue
			}
			// Check whether we can transmit to client
			clientCtx, ok = clientContextMap[clientID]
			if !ok {
				// First request for this client, ready to transmit
				rdy = true
			} else {
				// If there is no active context, the client is ready to transmit
				rdy = !clientCtx.isActive()
			}
		case clientID = <-deletedC:
			// The connection this client ID belonged to is gone. Drop its timeout so a
			// charge point that has already re-dialed under the same ID can transmit
			// straight away instead of waiting out the departed connection's timer.
			clientCtx = clientContextMap[clientID]
			delete(clientContextMap, clientID)
			if clientCtx.isActive() {
				clientCtx.cancel()
			}
			clientQueue, ok = d.queueMap.Get(clientID)
			rdy = ok
		case clientID, ok = <-timerC:
			// Timeout elapsed
			if !ok {
				continue
			}
			// Canceling timeout context
			log.Debugf("timeout for client %v, canceling message", clientID)
			clientCtx = clientContextMap[clientID]
			if clientCtx.isActive() {
				clientCtx.cancel()
				clientContextMap[clientID] = clientTimeoutContext{}
			}
			if d.pendingRequestState.HasPendingRequest(clientID) {
				// Current request for client timed out. Removing request and triggering cancel callback.
				// The queue may hold nothing to cancel: an answer for the very same request
				// can be completing it right now, having already taken it off the queue.
				q, ok := d.queueMap.Get(clientID)
				var bundle RequestBundle
				if ok {
					bundle, ok = q.Peek().(RequestBundle)
				}
				if ok {
					d.CompleteRequest(clientID, bundle.Call.UniqueId)
					log.Infof("request %v for %v timed out", bundle.Call.UniqueId, clientID)
					if d.onRequestCancel != nil {
						d.onRequestCancel(clientID, bundle.Call.UniqueId, bundle.Call.Payload,
							ocpp.NewError(GenericError, "Request timed out", bundle.Call.UniqueId))
					}
				} else {
					log.Infof("timeout for %v, but no request left to cancel", clientID)
				}
			}
		case clientID = <-d.readyForDispatch:
			// Cancel previous timeout (if any)
			clientCtx, ok = clientContextMap[clientID]
			if ok && clientCtx.isActive() {
				clientCtx.cancel()
				clientContextMap[clientID] = clientTimeoutContext{}
			}
			// Client can now transmit again
			clientQueue, ok = d.queueMap.Get(clientID)
			if ok {
				// Ready to transmit
				rdy = true
			}
			log.Debugf("%v ready to transmit again", clientID)
		}
		// Only dispatch request if able to send and request queue isn't empty
		if rdy && clientQueue != nil && !clientQueue.IsEmpty() {
			// Send request & set new context
			clientCtx = d.dispatchNextRequest(clientID)
			clientContextMap[clientID] = clientCtx
			if clientCtx.isActive() {
				go d.waitForTimeout(clientID, clientCtx, timerC, stoppedC)
			}
			// Update ready state
			rdy = false
		}
	}
}

func (d *DefaultServerDispatcher) dispatchNextRequest(clientID string) (clientCtx clientTimeoutContext) {
	// Get first element in queue
	q, ok := d.queueMap.Get(clientID)
	if !ok {
		log.Errorf("failed to dispatch next request for %s, no request queue available", clientID)
		return
	}
	el := q.Peek()
	bundle, _ := el.(RequestBundle)
	jsonMessage := bundle.Data
	callID := bundle.Call.GetUniqueId()
	d.pendingRequestState.AddPendingRequest(clientID, callID, bundle.Call.Payload)
	err := d.network.Write(clientID, jsonMessage)
	if err != nil {
		log.Errorf("error while sending message: %v", err)
		//TODO: handle retransmission instead of removing pending request
		d.CompleteRequest(clientID, callID)
		if d.onRequestCancel != nil {
			d.onRequestCancel(clientID, bundle.Call.UniqueId, bundle.Call.Payload,
				ocpp.NewError(InternalError, err.Error(), bundle.Call.UniqueId))
		}
		return
	}
	// Create and return context (only if timeout is set)
	if d.timeout > 0 {
		ctx, cancel := context.WithTimeout(context.TODO(), d.timeout)
		clientCtx = clientTimeoutContext{ctx: ctx, cancel: cancel}
	}
	log.Infof("dispatched request %s for %s", callID, clientID)
	log.Debugf("sent JSON message to %s: %s", clientID, string(jsonMessage))
	return
}

// waitForTimeout reports the timeout of clientCtx to the pump of the run that owns
// timerC and stoppedC. The channels are passed in rather than read from the dispatcher,
// because a Stop followed by a Start replaces them.
//
// No lock may be held while sending: the pump takes the dispatcher's lock for every
// event it handles, so a sender that blocks on a full timerC while holding it keeps the
// pump from ever draining the channel.
func (d *DefaultServerDispatcher) waitForTimeout(clientID string, clientCtx clientTimeoutContext, timerC chan<- string, stoppedC <-chan struct{}) {
	defer clientCtx.cancel()
	log.Debugf("started timeout timer for %s", clientID)
	select {
	case <-clientCtx.ctx.Done():
		err := clientCtx.ctx.Err()
		if err == context.DeadlineExceeded {
			// Timeout triggered, notifying messagePump. The wait is bounded by the
			// dispatcher stopping, so no goroutine outlives its run.
			select {
			case timerC <- clientID:
			case <-stoppedC:
			}
		} else {
			log.Debugf("timeout canceled for %s", clientID)
		}
	case <-stoppedC:
		// Server was stopped, every pending timeout gets canceled
	}
}

func (d *DefaultServerDispatcher) CompleteRequest(clientID string, requestID string) {
	q, ok := d.queueMap.Get(clientID)
	if !ok {
		log.Errorf("attempting to complete request for client %v, but no matching queue found", clientID)
		return
	}
	el := q.Peek()
	if el == nil {
		log.Errorf("attempting to pop front of queue, but queue is empty")
		return
	}
	bundle, _ := el.(RequestBundle)
	callID := bundle.Call.GetUniqueId()
	if callID != requestID {
		log.Errorf("internal state mismatch: processing response for %v but expected response for %v", requestID, callID)
		return
	}
	q.Pop()
	d.pendingRequestState.DeletePendingRequest(clientID, requestID)
	log.Debugf("completed request %s for %s", callID, clientID)
	// Signal that next message in queue may be sent
	d.signalReadyForDispatch(clientID)
}

// signalReadyForDispatch tells messagePump that clientID may transmit again.
//
// messagePump calls CompleteRequest itself while handling a request timeout or a write
// error, and it is the only reader of readyForDispatch, so a blocking send here wedges
// the pump against its own signal and stops every client on the server. The send must
// therefore never block the caller; when the buffer is busy, a sender goroutine waits
// on the pump's behalf instead.
func (d *DefaultServerDispatcher) signalReadyForDispatch(clientID string) {
	d.mutex.RLock()
	stoppedC := d.stoppedC
	d.mutex.RUnlock()
	signalClient(d.readyForDispatch, stoppedC, clientID)
}
