package ocppj_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocppj"
)

type ServerDispatcherTestSuite struct {
	suite.Suite
	mutex           sync.RWMutex
	state           ocppj.ServerState
	websocketServer MockWebsocketServer
	endpoint        ocppj.Server
	dispatcher      ocppj.ServerDispatcher
	queueMap        ocppj.ServerQueueMap
}

// stopDispatcher shuts the pump down and waits for it, so a dispatcher left over from
// one test cannot write to the mock the next test has already replaced. The wait is
// bounded: a wedged dispatcher never stops, and the test that wedged it has failed by
// the time this runs.
func stopDispatcher(d ocppj.ServerDispatcher) {
	stopped := make(chan struct{})
	go func() {
		d.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
	}
}

func (s *ServerDispatcherTestSuite) SetupTest() {
	s.endpoint = ocppj.Server{}
	mockProfile := ocpp.NewProfile("mock", &MockFeature{})
	s.endpoint.AddProfile(mockProfile)
	s.queueMap = ocppj.NewFIFOQueueMap(10)
	s.dispatcher = ocppj.NewDefaultServerDispatcher(s.queueMap)
	s.state = ocppj.NewServerState(&s.mutex)
	s.dispatcher.SetPendingRequestState(s.state)
	s.websocketServer = MockWebsocketServer{}
	s.dispatcher.SetNetworkServer(&s.websocketServer)
}

func (s *ServerDispatcherTestSuite) TestServerSendRequest() {
	t := s.T()
	// Setup
	clientID := "client1"
	sent := make(chan bool, 1)
	s.websocketServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		assert.Equal(t, clientID, id)
		sent <- true
	}).Return(nil)
	timeout := time.Second * 1
	s.dispatcher.SetTimeout(timeout)
	s.dispatcher.SetOnRequestCanceled(func(cID string, rID string, request ocpp.Request, err *ocpp.Error) {
		require.Fail(t, "unexpected OnRequestCanceled")
	})
	s.dispatcher.Start()
	require.True(t, s.dispatcher.IsRunning())
	// Simulate client connection
	s.dispatcher.CreateClient(clientID)
	// Create and send mock request
	req := newMockRequest("somevalue")
	call, err := s.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	err = s.dispatcher.SendRequest(clientID, bundle)
	require.NoError(t, err)
	// Check underlying queue
	q, ok := s.queueMap.Get(clientID)
	require.True(t, ok)
	assert.False(t, q.IsEmpty())
	assert.Equal(t, 1, q.Size())
	// Wait for websocket to send message
	_, ok = <-sent
	assert.True(t, ok)
	assert.True(t, s.state.HasPendingRequest(clientID))
	// Complete request
	s.dispatcher.CompleteRequest(clientID, requestID)
	assert.False(t, s.state.HasPendingRequest(clientID))
	assert.True(t, q.IsEmpty())
	// Assert that no timeout is invoked
	time.Sleep(1300 * time.Millisecond)
}

func (s *ServerDispatcherTestSuite) TestServerRequestCanceled() {
	t := s.T()
	// Setup
	clientID := "client1"
	canceled := make(chan bool, 1)
	writeC := make(chan bool, 1)
	errMsg := "mockError"
	// Mock write error to trigger onRequestCanceled
	// This never starts a timeout
	s.websocketServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		assert.Equal(t, clientID, id)
		<-writeC
	}).Return(fmt.Errorf(errMsg))
	// Create mock request
	req := newMockRequest("somevalue")
	call, err := s.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	// Set canceled callback
	s.dispatcher.SetOnRequestCanceled(func(cID string, rID string, request ocpp.Request, err *ocpp.Error) {
		assert.Equal(t, clientID, cID)
		assert.Equal(t, requestID, rID)
		assert.Equal(t, MockFeatureName, request.GetFeatureName())
		assert.Equal(t, req, request)
		assert.Equal(t, ocppj.InternalError, err.Code)
		assert.Equal(t, errMsg, err.Description)
		canceled <- true
	})
	s.dispatcher.Start()
	require.True(t, s.dispatcher.IsRunning())
	// Simulate client connection
	s.dispatcher.CreateClient(clientID)
	// Send mock request
	err = s.dispatcher.SendRequest(clientID, bundle)
	require.NoError(t, err)
	// Check underlying queue
	time.Sleep(100 * time.Millisecond)
	q, ok := s.queueMap.Get(clientID)
	require.True(t, ok)
	assert.False(t, q.IsEmpty())
	assert.Equal(t, 1, q.Size())
	assert.True(t, s.state.HasPendingRequest(clientID))
	// Signal that write can occur now, then check canceled request
	writeC <- true
	_, ok = <-canceled
	require.True(t, ok)
	assert.False(t, s.state.HasPendingRequest(clientID))
	assert.True(t, q.IsEmpty())
}

func (s *ServerDispatcherTestSuite) TestCreateClient() {
	t := s.T()
	// Setup
	clientID := "client1"
	s.dispatcher.Start()
	require.True(t, s.dispatcher.IsRunning())
	// No client state created yet
	_, ok := s.queueMap.Get(clientID)
	assert.False(t, ok)
	// Create client state
	s.dispatcher.CreateClient(clientID)
	_, ok = s.queueMap.Get(clientID)
	assert.True(t, ok)
	assert.False(t, s.state.HasPendingRequest(clientID))
}

func (s *ServerDispatcherTestSuite) TestDeleteClient() {
	t := s.T()
	// Setup
	clientID := "client1"
	sent := make(chan bool, 1)
	s.websocketServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		assert.Equal(t, clientID, id)
		sent <- true
	}).Return(nil)
	s.dispatcher.Start()
	require.True(t, s.dispatcher.IsRunning())
	// Simulate client connection
	s.dispatcher.CreateClient(clientID)
	// Create and send mock request
	req := newMockRequest("somevalue")
	call, err := s.endpoint.CreateCall(req)
	require.NoError(t, err)
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	err = s.dispatcher.SendRequest(clientID, bundle)
	require.NoError(t, err)
	// Wait for websocket to send message
	_, ok := <-sent
	assert.True(t, ok)
	// Delete client
	s.dispatcher.DeleteClient(clientID)
	// Pending request is still expected to be there
	assert.True(t, s.state.HasPendingRequest(clientID))
}

func (s *ServerDispatcherTestSuite) TestServerDispatcherTimeout() {
	t := s.T()
	// Setup
	clientID := "client1"
	canceled := make(chan bool, 1)
	s.websocketServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		assert.Equal(t, clientID, id)
	}).Return(nil)
	// Create mock request
	req := newMockRequest("somevalue")
	call, err := s.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	// Set canceled callback
	s.dispatcher.SetOnRequestCanceled(func(cID string, rID string, request ocpp.Request, err *ocpp.Error) {
		assert.Equal(t, clientID, cID)
		assert.Equal(t, requestID, rID)
		assert.Equal(t, MockFeatureName, request.GetFeatureName())
		assert.Equal(t, req, request)
		assert.Equal(t, ocppj.GenericError, err.Code)
		assert.Equal(t, "Request timed out", err.Description)
		canceled <- true
	})
	// Set timeout and start
	timeout := time.Second * 1
	s.dispatcher.SetTimeout(timeout)
	s.dispatcher.Start()
	require.True(t, s.dispatcher.IsRunning())
	// Simulate client connection
	s.dispatcher.CreateClient(clientID)
	// Send mock request
	startTime := time.Now()
	err = s.dispatcher.SendRequest(clientID, bundle)
	require.NoError(t, err)
	// Wait for timeout, canceled callback will be invoked
	_, ok := <-canceled
	assert.True(t, ok)
	elapsed := time.Since(startTime)
	assert.GreaterOrEqual(t, elapsed.Seconds(), timeout.Seconds())
	clientQ, _ := s.queueMap.Get(clientID)
	assert.True(t, clientQ.IsEmpty())
}

// A request timeout makes messagePump call CompleteRequest from inside its own loop,
// and CompleteRequest signals readyForDispatch, a channel only messagePump reads.
// While concurrent responses keep that channel occupied, the pump must not block on
// its own signal: doing so stops every OCPP message on the server (prod, 2026-08-18).
func (s *ServerDispatcherTestSuite) TestServerDispatcherStaysAliveOnTimeoutDuringResponseStorm() {
	t := s.T()
	const (
		silentClients     = 8
		requestsPerSilent = 8
		stormClients      = 8
		timeout           = 200 * time.Millisecond
		stormDuration     = 2 * time.Second
	)
	// Queues must hold every preloaded request of a silent client.
	s.queueMap = ocppj.NewFIFOQueueMap(requestsPerSilent)
	s.dispatcher = ocppj.NewDefaultServerDispatcher(s.queueMap)
	s.dispatcher.SetPendingRequestState(s.state)
	s.dispatcher.SetNetworkServer(&s.websocketServer)
	s.dispatcher.SetTimeout(timeout)
	// Storm clients only answer requests that were actually put on the wire, as a real
	// charge point does. The channels are all created before the dispatcher starts.
	const canaryID = "canary"
	dispatched := map[string]chan struct{}{canaryID: make(chan struct{}, 1)}
	for i := 0; i < stormClients; i++ {
		dispatched[fmt.Sprintf("storm%d", i)] = make(chan struct{}, 1)
	}
	networkServer := &MockWebsocketServer{}
	s.dispatcher.SetNetworkServer(networkServer)
	networkServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		if ch, ok := dispatched[id]; ok {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}).Return(nil)
	canaryCanceled := make(chan struct{}, 1)
	s.dispatcher.SetOnRequestCanceled(func(cID string, rID string, request ocpp.Request, err *ocpp.Error) {
		if cID == canaryID {
			select {
			case canaryCanceled <- struct{}{}:
			default:
			}
		}
	})
	s.dispatcher.Start()
	// Stop() needs the dispatcher's lock, which a wedged dispatcher never releases, so
	// cleanup runs in the background and never holds up the assertions below.
	defer stopDispatcher(s.dispatcher)
	newBundle := func() (ocppj.RequestBundle, string) {
		call, err := s.endpoint.CreateCall(newMockRequest("somevalue"))
		require.NoError(t, err)
		data, err := call.MarshalJSON()
		require.NoError(t, err)
		return ocppj.RequestBundle{Call: call, Data: data}, call.UniqueId
	}
	// Silent clients never answer, so each dispatched request times out and the next
	// one is dispatched right after — a steady supply of in-pump CompleteRequest calls.
	for i := 0; i < silentClients; i++ {
		clientID := fmt.Sprintf("silent%d", i)
		s.dispatcher.CreateClient(clientID)
		for j := 0; j < requestsPerSilent; j++ {
			bundle, _ := newBundle()
			require.NoError(t, s.dispatcher.SendRequest(clientID, bundle))
		}
	}
	// Storm clients answer immediately, keeping readyForDispatch occupied. They run in
	// their own goroutines and are left behind if the dispatcher stops making progress.
	var storm sync.WaitGroup
	for i := 0; i < stormClients; i++ {
		clientID := fmt.Sprintf("storm%d", i)
		s.dispatcher.CreateClient(clientID)
		storm.Add(1)
		go func() {
			defer storm.Done()
			deadline := time.Now().Add(stormDuration)
			for time.Now().Before(deadline) {
				bundle, requestID := newBundle()
				if err := s.dispatcher.SendRequest(clientID, bundle); err != nil {
					return
				}
				<-dispatched[clientID]
				s.dispatcher.CompleteRequest(clientID, requestID)
			}
		}()
	}
	time.Sleep(stormDuration)
	// A request arriving after the storm must still reach the wire and still time out.
	s.dispatcher.CreateClient(canaryID)
	canaryBundle, _ := newBundle()
	go func() { _ = s.dispatcher.SendRequest(canaryID, canaryBundle) }()
	select {
	case <-dispatched[canaryID]:
	case <-time.After(2 * time.Second):
		require.Fail(t, "dispatcher stopped dispatching requests")
	}
	select {
	case <-canaryCanceled:
	case <-time.After(2 * time.Second):
		require.Fail(t, "dispatcher stopped timing out requests")
	}
	// Let the storm goroutines retire before the suite hands the mock to the next test.
	stormDone := make(chan struct{})
	go func() { storm.Wait(); close(stormDone) }()
	select {
	case <-stormDone:
	case <-time.After(2 * time.Second):
	}
}

// A charge point that drops off the network while a request is in flight has its queue
// removed, but the pending request outlives it and still times out. Handling that
// timeout must not reach into the queue that is no longer there: the dispatcher runs a
// whole fleet, so a panic here takes every other charge point down with it.
func (s *ServerDispatcherTestSuite) TestServerDispatcherTimeoutAfterClientDeleted() {
	t := s.T()
	const (
		goneID  = "gone"
		aliveID = "alive"
		timeout = 200 * time.Millisecond
	)
	dispatched := map[string]chan struct{}{
		goneID:  make(chan struct{}, 1),
		aliveID: make(chan struct{}, 1),
	}
	networkServer := &MockWebsocketServer{}
	s.dispatcher.SetNetworkServer(networkServer)
	networkServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		if ch, ok := dispatched[id]; ok {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}).Return(nil)
	s.dispatcher.SetTimeout(timeout)
	s.dispatcher.Start()
	defer stopDispatcher(s.dispatcher)
	newBundle := func() ocppj.RequestBundle {
		call, err := s.endpoint.CreateCall(newMockRequest("somevalue"))
		require.NoError(t, err)
		data, err := call.MarshalJSON()
		require.NoError(t, err)
		return ocppj.RequestBundle{Call: call, Data: data}
	}
	// Send a request, then drop the client before the answer (or the timeout) arrives.
	s.dispatcher.CreateClient(goneID)
	require.NoError(t, s.dispatcher.SendRequest(goneID, newBundle()))
	<-dispatched[goneID]
	s.dispatcher.DeleteClient(goneID)
	require.True(t, s.state.HasPendingRequest(goneID))
	// Another charge point must be served normally while that timeout elapses.
	time.Sleep(2 * timeout)
	s.dispatcher.CreateClient(aliveID)
	go func() { _ = s.dispatcher.SendRequest(aliveID, newBundle()) }()
	select {
	case <-dispatched[aliveID]:
	case <-time.After(2 * time.Second):
		require.Fail(t, "dispatcher stopped serving other clients")
	}
}

// A charge point that re-dials takes over its own client ID: the server tears the old
// client down and sets the new one up right away, while the pump may still be busy
// elsewhere. The request the new connection sends must go out on its own account, not
// wait for a timer that belongs to the connection that is already gone.
func (s *ServerDispatcherTestSuite) TestServerDispatcherRequestAfterClientTakeover() {
	t := s.T()
	const (
		clientID  = "cp"
		blockerID = "blocker"
		timeout   = 3 * time.Second
	)
	writing := make(chan struct{}, 1)
	releaseWrite := make(chan struct{})
	dispatched := make(chan struct{}, 4)
	networkServer := &MockWebsocketServer{}
	s.dispatcher.SetNetworkServer(networkServer)
	networkServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		if id, _ := args.Get(0).(string); id == blockerID {
			writing <- struct{}{}
			<-releaseWrite
			return
		}
		dispatched <- struct{}{}
	}).Return(nil)
	s.dispatcher.SetTimeout(timeout)
	s.dispatcher.Start()
	defer stopDispatcher(s.dispatcher)
	newBundle := func() ocppj.RequestBundle {
		call, err := s.endpoint.CreateCall(newMockRequest("somevalue"))
		require.NoError(t, err)
		data, err := call.MarshalJSON()
		require.NoError(t, err)
		return ocppj.RequestBundle{Call: call, Data: data}
	}
	// The old connection has a request in flight, so a timeout is armed for this ID.
	s.dispatcher.CreateClient(clientID)
	require.NoError(t, s.dispatcher.SendRequest(clientID, newBundle()))
	<-dispatched
	// Keep the pump busy so the whole takeover lands in one go, as it does when a
	// reconnect storm keeps the dispatcher occupied.
	s.dispatcher.CreateClient(blockerID)
	require.NoError(t, s.dispatcher.SendRequest(blockerID, newBundle()))
	<-writing
	// Takeover, in the order ocppj's server performs it: disconnect, then connect.
	s.dispatcher.DeleteClient(clientID)
	s.state.ClearClientPendingRequest(clientID)
	s.dispatcher.CreateClient(clientID)
	require.NoError(t, s.dispatcher.SendRequest(clientID, newBundle()))
	close(releaseWrite)
	select {
	case <-dispatched:
	case <-time.After(timeout / 2):
		require.Fail(t, "request from the new connection was not dispatched")
	}
}

// slowDeleteState holds a request in the pending state after its queue entry is gone,
// which is the window CompleteRequest passes through on every answered request.
type slowDeleteState struct {
	ocppj.ServerState
	deleting chan struct{}
	release  chan struct{}
}

func (st *slowDeleteState) DeletePendingRequest(clientID string, requestID string) {
	select {
	case st.deleting <- struct{}{}:
		<-st.release
	default:
	}
	st.ServerState.DeletePendingRequest(clientID, requestID)
}

// An answer and a timeout can reach the dispatcher for the same request at the same
// time: the answer empties the queue while the timeout still sees a pending request.
// The timeout must cope with the empty queue instead of taking the whole fleet down.
func (s *ServerDispatcherTestSuite) TestServerDispatcherTimeoutRacingWithResponse() {
	t := s.T()
	const (
		clientID = "client1"
		aliveID  = "alive"
		timeout  = 200 * time.Millisecond
	)
	state := &slowDeleteState{
		ServerState: ocppj.NewServerState(&s.mutex),
		deleting:    make(chan struct{}, 1),
		release:     make(chan struct{}),
	}
	s.dispatcher.SetPendingRequestState(state)
	dispatched := map[string]chan struct{}{
		clientID: make(chan struct{}, 1),
		aliveID:  make(chan struct{}, 1),
	}
	networkServer := &MockWebsocketServer{}
	s.dispatcher.SetNetworkServer(networkServer)
	networkServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		id, _ := args.Get(0).(string)
		if ch, ok := dispatched[id]; ok {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}).Return(nil)
	s.dispatcher.SetTimeout(timeout)
	s.dispatcher.Start()
	defer stopDispatcher(s.dispatcher)
	newBundle := func() (ocppj.RequestBundle, string) {
		call, err := s.endpoint.CreateCall(newMockRequest("somevalue"))
		require.NoError(t, err)
		data, err := call.MarshalJSON()
		require.NoError(t, err)
		return ocppj.RequestBundle{Call: call, Data: data}, call.UniqueId
	}
	s.dispatcher.CreateClient(clientID)
	bundle, requestID := newBundle()
	require.NoError(t, s.dispatcher.SendRequest(clientID, bundle))
	<-dispatched[clientID]
	// The answer arrives and empties the queue, but stalls before clearing the pending
	// request, so the timeout below lands in the middle of that window.
	go s.dispatcher.CompleteRequest(clientID, requestID)
	<-state.deleting
	time.Sleep(2 * timeout)
	close(state.release)
	// Another charge point must still be served.
	s.dispatcher.CreateClient(aliveID)
	aliveBundle, _ := newBundle()
	go func() { _ = s.dispatcher.SendRequest(aliveID, aliveBundle) }()
	select {
	case <-dispatched[aliveID]:
	case <-time.After(2 * time.Second):
		require.Fail(t, "dispatcher stopped serving other clients")
	}
}

type ClientDispatcherTestSuite struct {
	suite.Suite
	state           ocppj.ClientState
	queue           ocppj.RequestQueue
	dispatcher      ocppj.ClientDispatcher
	endpoint        ocppj.Client
	websocketClient MockWebsocketClient
}

func (c *ClientDispatcherTestSuite) SetupTest() {
	c.endpoint = ocppj.Client{Id: "client1"}
	mockProfile := ocpp.NewProfile("mock", &MockFeature{})
	c.endpoint.AddProfile(mockProfile)
	c.queue = ocppj.NewFIFOClientQueue(10)
	c.dispatcher = ocppj.NewDefaultClientDispatcher(c.queue)
	c.state = ocppj.NewClientState()
	c.dispatcher.SetPendingRequestState(c.state)
	c.websocketClient = MockWebsocketClient{}
	c.dispatcher.SetNetworkClient(&c.websocketClient)
}

func (c *ClientDispatcherTestSuite) TestClientSendRequest() {
	t := c.T()
	// Setup
	sent := make(chan bool, 1)
	c.websocketClient.On("Write", mock.Anything).Run(func(args mock.Arguments) {
		sent <- true
	}).Return(nil)
	c.dispatcher.Start()
	require.True(t, c.dispatcher.IsRunning())
	// Create and send mock request
	req := newMockRequest("somevalue")
	call, err := c.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	err = c.dispatcher.SendRequest(bundle)
	require.NoError(t, err)
	// Check underlying queue
	assert.False(t, c.queue.IsEmpty())
	assert.Equal(t, 1, c.queue.Size())
	// Wait for websocket to send message
	_, ok := <-sent
	assert.True(t, ok)
	assert.True(t, c.state.HasPendingRequest())
	// Complete request
	c.dispatcher.CompleteRequest(requestID)
	assert.False(t, c.state.HasPendingRequest())
	assert.True(t, c.queue.IsEmpty())

}

func (c *ClientDispatcherTestSuite) TestClientRequestCanceled() {
	t := c.T()
	// Setup
	canceled := make(chan bool, 1)
	writeC := make(chan bool, 1)
	errMsg := "mockError"
	c.websocketClient.On("Write", mock.Anything).Run(func(args mock.Arguments) {
		<-writeC
	}).Return(fmt.Errorf(errMsg))
	// Create mock request
	req := newMockRequest("somevalue")
	call, err := c.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	// Set canceled callback
	c.dispatcher.SetOnRequestCanceled(func(rID string, request ocpp.Request, err *ocpp.Error) {
		assert.Equal(t, requestID, rID)
		assert.Equal(t, MockFeatureName, request.GetFeatureName())
		assert.Equal(t, req, request)
		assert.Equal(t, ocppj.InternalError, err.Code)
		assert.Equal(t, errMsg, err.Description)
		canceled <- true
	})
	c.dispatcher.Start()
	require.True(t, c.dispatcher.IsRunning())
	// Send mock request
	err = c.dispatcher.SendRequest(bundle)
	require.NoError(t, err)
	// Check underlying queue
	time.Sleep(100 * time.Millisecond)
	assert.False(t, c.queue.IsEmpty())
	assert.Equal(t, 1, c.queue.Size())
	assert.True(t, c.state.HasPendingRequest())
	// Signal that write can occur now, then check canceled request
	writeC <- true
	_, ok := <-canceled
	require.True(t, ok)
	assert.False(t, c.state.HasPendingRequest())
	assert.True(t, c.queue.IsEmpty())
}

func (c *ClientDispatcherTestSuite) TestClientDispatcherTimeout() {
	t := c.T()
	// Setup
	writeC := make(chan bool, 1)
	timeout := make(chan bool, 1)
	c.websocketClient.On("Write", mock.Anything).Run(func(args mock.Arguments) {
		writeC <- true
	}).Return(nil)
	// Create mock request
	req := newMockRequest("somevalue")
	call, err := c.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	// Set low timeout to trigger OnRequestCanceled callback
	c.dispatcher.SetTimeout(1 * time.Second)
	c.dispatcher.SetOnRequestCanceled(func(rID string, request ocpp.Request, err *ocpp.Error) {
		assert.Equal(t, requestID, rID)
		assert.Equal(t, MockFeatureName, request.GetFeatureName())
		assert.Equal(t, req, request)
		assert.Equal(t, ocppj.GenericError, err.Code)
		assert.Equal(t, "Request timed out", err.Description)
		timeout <- true
	})
	c.dispatcher.Start()
	require.True(t, c.dispatcher.IsRunning())
	// Send mocked request
	err = c.dispatcher.SendRequest(bundle)
	require.NoError(t, err)
	// Check status after sending request
	<-writeC
	assert.True(t, c.state.HasPendingRequest())
	// Wait for timeout
	_, ok := <-timeout
	assert.True(t, ok)
	assert.False(t, c.state.HasPendingRequest())
	assert.True(t, c.queue.IsEmpty())
}

func (c *ClientDispatcherTestSuite) TestClientPauseDispatcher() {
	t := c.T()
	// Create mock request
	timeout := make(chan bool, 1)
	c.websocketClient.On("Write", mock.Anything).Return(nil)
	req := newMockRequest("somevalue")
	call, err := c.endpoint.CreateCall(req)
	require.NoError(t, err)
	requestID := call.UniqueId
	data, err := call.MarshalJSON()
	require.NoError(t, err)
	bundle := ocppj.RequestBundle{Call: call, Data: data}
	// Set timeout to test pause functionality
	c.dispatcher.SetTimeout(500 * time.Millisecond)
	// The callback will only be triggered at the end of the test case
	c.dispatcher.SetOnRequestCanceled(func(rID string, request ocpp.Request, err *ocpp.Error) {
		assert.Equal(t, requestID, rID)
		assert.Equal(t, MockFeatureName, request.GetFeatureName())
		assert.Equal(t, req, request)
		timeout <- true
	})
	c.dispatcher.Start()
	require.True(t, c.dispatcher.IsRunning())
	err = c.dispatcher.SendRequest(bundle)
	require.NoError(t, err)
	// Pause and attempt retransmission 2 times
	for i := 0; i < 2; i++ {
		time.Sleep(200 * time.Millisecond)
		// Pause dispatcher
		c.dispatcher.Pause()
		assert.True(t, c.dispatcher.IsPaused())
		// Elapsed time since start ~ 1 second, no timeout should be triggered (set to 0.5 seconds)
		time.Sleep(800 * time.Millisecond)
		assert.True(t, c.state.HasPendingRequest())
		assert.False(t, c.queue.IsEmpty())
		// Resume and restart transmission timer
		c.dispatcher.Resume()
		assert.False(t, c.dispatcher.IsPaused())
	}
	// Wait for timeout
	_, ok := <-timeout
	assert.True(t, ok)
	assert.False(t, c.state.HasPendingRequest())
	assert.True(t, c.queue.IsEmpty())
}

func (c *ClientDispatcherTestSuite) TestClientSendPausedDispatcher() {
	t := c.T()
	// Create mock request
	c.websocketClient.On("Write", mock.Anything).Run(func(args mock.Arguments) {
		require.Fail(t, "write should never be called")
	}).Return(nil)
	// Set timeout (unused for this test)
	c.dispatcher.SetTimeout(1 * time.Second)
	// The callback will only be triggered at the end of the test case
	c.dispatcher.SetOnRequestCanceled(func(rID string, request ocpp.Request, err *ocpp.Error) {
		require.Fail(t, "unexpected OnRequestCanceled")
	})
	c.dispatcher.Start()
	require.True(t, c.dispatcher.IsRunning())
	// Pause, then send request
	c.dispatcher.Pause()
	assert.False(t, c.state.HasPendingRequest())
	assert.True(t, c.queue.IsEmpty())
	requestIDs := []string{}
	requestNumber := 2
	for i := 0; i < requestNumber; i++ {
		req := newMockRequest("somevalue")
		call, err := c.endpoint.CreateCall(req)
		require.NoError(t, err)
		requestID := call.UniqueId
		data, err := call.MarshalJSON()
		require.NoError(t, err)
		bundle := ocppj.RequestBundle{Call: call, Data: data}
		err = c.dispatcher.SendRequest(bundle)
		require.NoError(t, err)
		requestIDs = append(requestIDs, requestID)
	}
	time.Sleep(500 * time.Millisecond)
	// Request is queued
	assert.Equal(t, requestNumber, c.queue.Size())
	assert.False(t, c.state.HasPendingRequest())
	// After waiting for some time, no timeout was triggered and no pending requests
	time.Sleep(1 * time.Second)
	assert.Equal(t, requestNumber, c.queue.Size())
	assert.False(t, c.state.HasPendingRequest())
}

// A charge point's Stop can race a heartbeat that is just being sent — every
// disconnect on a live charger does this. The send must fail with an error, not
// bring the process down on the closed request channel.
func TestClientDispatcherSendRequestDuringStopDoesNotPanic(t *testing.T) {
	for round := 0; round < 200; round++ {
		queue := ocppj.NewFIFOClientQueue(10)
		dispatcher := ocppj.NewDefaultClientDispatcher(queue)
		dispatcher.SetPendingRequestState(ocppj.NewClientState())
		endpoint := ocppj.Client{}
		endpoint.AddProfile(ocpp.NewProfile("mock", &MockFeature{}))
		websocketClient := MockWebsocketClient{}
		websocketClient.On("Write", mock.Anything).Return(nil)
		dispatcher.SetNetworkClient(&websocketClient)
		dispatcher.Start()

		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 0; i < 20; i++ {
				call, err := endpoint.CreateCall(newMockRequest("somevalue"))
				require.NoError(t, err)
				data, err := call.MarshalJSON()
				require.NoError(t, err)
				// The error is expected once the dispatcher stops; the panic is not.
				_ = dispatcher.SendRequest(ocppj.RequestBundle{Call: call, Data: data})
			}
		}()
		dispatcher.Stop()
		<-done
	}
}

// Many charge points that never answer make their request timeouts fire together. The
// timeout goroutines must hand the event to the pump without holding the dispatcher's
// lock, which the pump itself needs to drain them: a server that handles several
// thousand charge points would otherwise freeze for good after a mass reconnect.
// Thousands of readers of the client state run alongside, as one per connection does.
func TestServerDispatcherSurvivesMassTimeout(t *testing.T) {
	const (
		clients = 150
		readers = 3000
		timeout = 100 * time.Millisecond
	)
	// The default pending request state is deliberately kept: it shares the dispatcher's
	// own lock, as it does in a real server.
	dispatcher := ocppj.NewDefaultServerDispatcher(ocppj.NewFIFOQueueMap(1))
	dispatcher.SetTimeout(timeout)
	var mutex sync.Mutex
	written := map[string]bool{}
	networkServer := &MockWebsocketServer{}
	endpoint := ocppj.NewServer(networkServer, dispatcher, nil, ocpp.NewProfile("mock", &MockFeature{}))
	networkServer.On("Write", mock.AnythingOfType("string"), mock.Anything).Run(func(args mock.Arguments) {
		mutex.Lock()
		defer mutex.Unlock()
		written[args.String(0)] = true
	}).Return(nil)
	canceled := make(chan string, clients+1)
	dispatcher.SetOnRequestCanceled(func(cID string, rID string, request ocpp.Request, err *ocpp.Error) {
		// A pump that is busy with one timeout while the others queue up is what a
		// loaded server looks like; it keeps the timeout channel full.
		time.Sleep(time.Millisecond)
		canceled <- cID
	})
	dispatcher.Start()
	// A wedged dispatcher never releases its lock, so cleanup must not wait for it.
	defer stopDispatcher(dispatcher)
	newBundle := func() ocppj.RequestBundle {
		call, err := endpoint.CreateCall(newMockRequest("somevalue"))
		require.NoError(t, err)
		data, err := call.MarshalJSON()
		require.NoError(t, err)
		return ocppj.RequestBundle{Call: call, Data: data}
	}
	// Every incoming message of every charge point looks up its client state. These
	// readers share the dispatcher's lock and must not hold the pump back.
	stopReaders := make(chan struct{})
	defer close(stopReaders)
	for i := 0; i < readers; i++ {
		clientID := fmt.Sprintf("silent%d", i%clients)
		if i%3 == 0 {
			clientID = fmt.Sprintf("late%d", i) // first use creates the entry
		}
		go func() {
			for {
				select {
				case <-stopReaders:
					return
				default:
				}
				_ = endpoint.RequestState.GetClientState(clientID)
				time.Sleep(10 * time.Millisecond)
			}
		}()
	}
	// Queue a request for every client at once; none of them is ever answered.
	senders := sync.WaitGroup{}
	for i := 0; i < clients; i++ {
		clientID := fmt.Sprintf("silent%d", i)
		dispatcher.CreateClient(clientID)
		bundle := newBundle()
		senders.Add(1)
		go func() {
			defer senders.Done()
			_ = dispatcher.SendRequest(clientID, bundle)
		}()
	}
	watchdog := time.After(6 * time.Second)
	// Every request must be canceled by its timeout.
	for i := 0; i < clients; i++ {
		select {
		case <-canceled:
		case <-watchdog:
			require.Fail(t, "dispatcher stopped handling timeouts", "only %d of %d requests timed out", i, clients)
		}
	}
	// The dispatcher must still serve new work: a new client and a new request.
	probe := make(chan struct{})
	go func() {
		defer close(probe)
		dispatcher.CreateClient("probe")
		_ = dispatcher.SendRequest("probe", newBundle())
		_ = endpoint.RequestState.GetClientState("probe")
		_ = dispatcher.IsRunning()
	}()
	select {
	case <-probe:
	case <-watchdog:
		require.Fail(t, "dispatcher is deadlocked")
	}
	for {
		mutex.Lock()
		ok := written["probe"]
		mutex.Unlock()
		if ok {
			break
		}
		select {
		case <-watchdog:
			require.Fail(t, "dispatcher stopped dispatching requests")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
