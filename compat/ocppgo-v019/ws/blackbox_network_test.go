// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package ws_test

import (
	"fmt"
	. "github.com/lorenzodonini/ocpp-go/ws"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	toxiproxy "github.com/Shopify/toxiproxy/client"
)

type NetworkTestSuite struct {
	suite.Suite
	proxy     *toxiproxy.Proxy
	proxyPort int
	server    *Server
	client    *Client
	// The timeout configurations are kept here and applied with SetTimeoutConfig
	// (the upstream test wrote the unexported fields).
	clientCfg ClientTimeoutConfig
	serverCfg ServerTimeoutConfig
}

func (s *NetworkTestSuite) apply() {
	s.client.SetTimeoutConfig(s.clientCfg)
	s.server.SetTimeoutConfig(s.serverCfg)
}

func (s *NetworkTestSuite) SetupSuite() {
	client := toxiproxy.NewClient("localhost:8474")
	if _, err := client.Proxies(); err != nil {
		s.T().Skipf("no toxiproxy server on localhost:8474: %v", err)
	}
	s.proxyPort = 8886
	// Proxy listens on 8886 and upstreams to 8887 (where ocpp server is actually listening)
	oldProxy, err := client.Proxy("ocpp")
	if oldProxy != nil {
		oldProxy.Delete()
	}
	p, err := client.CreateProxy("ocpp", "localhost:8886", fmt.Sprintf("localhost:%v", serverPort))
	require.NoError(s.T(), err)
	s.proxy = p
}

func (s *NetworkTestSuite) TearDownSuite() {
	s.proxy.Delete()
}

func (s *NetworkTestSuite) SetupTest() {
	s.server = newWebsocketServer(s.T(), nil)
	s.client = newWebsocketClient(s.T(), nil)
	s.clientCfg = NewClientTimeoutConfig()
	s.serverCfg = NewServerTimeoutConfig()
}

func (s *NetworkTestSuite) TearDownTest() {
	s.server = nil
	s.client = nil
}

func (s *NetworkTestSuite) TestClientConnectionFailed() {
	t := s.T()
	s.server = newWebsocketServer(t, nil)
	s.server.SetNewClientHandler(func(ws Channel) {
		assert.Fail(t, "should not accept new clients")
	})
	s.apply()
	go s.server.Start(serverPort, serverPath)
	time.Sleep(500 * time.Millisecond)

	// Test client
	host := s.proxy.Listen
	u := url.URL{Scheme: "ws", Host: host, Path: testPath}

	// Disable network
	_ = s.proxy.Disable()
	defer s.proxy.Enable()
	// Attempt connection
	err := s.client.Start(u.String())
	require.Error(t, err)
	netError, ok := err.(*net.OpError)
	require.True(t, ok)
	require.NotNil(t, netError.Err)
	sysError, ok := netError.Err.(*os.SyscallError)
	require.True(t, ok)
	assert.Equal(t, "connect", sysError.Syscall)
	assert.Equal(t, syscall.ECONNREFUSED, sysError.Err)
	// Cleanup
	s.server.Stop()
}

func (s *NetworkTestSuite) TestClientConnectionFailedTimeout() {
	t := s.T()
	// Set timeouts for test
	s.clientCfg.HandshakeTimeout = 2 * time.Second
	s.client.SetTimeoutConfig(s.clientCfg)
	// Setup
	s.server = newWebsocketServer(t, nil)
	s.server.SetNewClientHandler(func(ws Channel) {
		assert.Fail(t, "should not accept new clients")
	})
	s.apply()
	go s.server.Start(serverPort, serverPath)
	time.Sleep(500 * time.Millisecond)

	// Test client
	host := s.proxy.Listen
	u := url.URL{Scheme: "ws", Host: host, Path: testPath}

	// Add connection timeout
	_, err := s.proxy.AddToxic("connectTimeout", "timeout", "upstream", 1, toxiproxy.Attributes{
		"timeout": 3000, // 3 seconds
	})
	defer s.proxy.RemoveToxic("connectTimeout")
	require.NoError(t, err)
	// Attempt connection
	err = s.client.Start(u.String())
	require.Error(t, err)
	netError, ok := err.(*net.OpError)
	require.True(t, ok)
	require.NotNil(t, netError.Err)
	assert.True(t, strings.Contains(netError.Error(), "timeout"))
	assert.True(t, netError.Timeout())
	// Cleanup
	s.server.Stop()
}

func (s *NetworkTestSuite) TestClientAutoReconnect() {
	t := s.T()
	// Set timeouts for test
	// ocpp-go v0.19 has no ReconnectBackoff (its client retries through StartWithRetries); the
	// client side is upstream's code, so this test of its auto reconnect is not ported.
	t.Skip("client auto reconnect of ocpp-go v0.17 only")
	// Setup
	serverOnDisconnected := make(chan bool, 1)
	clientOnDisconnected := make(chan bool, 1)
	reconnected := make(chan bool, 1)
	s.server = newWebsocketServer(t, nil)
	s.server.SetNewClientHandler(func(ws Channel) {
		assert.NotNil(t, ws)
	})
	s.server.SetDisconnectedClientHandler(func(ws Channel) {
		serverOnDisconnected <- true
	})
	s.apply()
	go s.server.Start(serverPort, serverPath)
	time.Sleep(500 * time.Millisecond)

	// Test bench
	s.client.SetDisconnectedHandler(func(err error) {
		assert.NotNil(t, err)
		closeError, ok := err.(*websocket.CloseError)
		require.True(t, ok)
		assert.Equal(t, websocket.CloseAbnormalClosure, closeError.Code)
		assert.False(t, s.client.IsConnected())
		clientOnDisconnected <- true
	})
	s.client.SetReconnectedHandler(func() {
		time.Sleep(50 * time.Millisecond) // Make sure we reconnected after backoff
		reconnected <- true
	})
	// Connect client
	host := s.proxy.Listen
	u := url.URL{Scheme: "ws", Host: host, Path: testPath}
	err := s.client.Start(u.String())
	require.Nil(t, err)
	// Close all connection from server side
	time.Sleep(500 * time.Millisecond)
	// Drop the TCP connections between client and server (abnormal closure on both ends).
	require.NoError(t, s.proxy.Disable())
	require.NoError(t, s.proxy.Enable())
	// Wait for disconnect to propagate
	result := <-serverOnDisconnected
	require.True(t, result)
	result = <-clientOnDisconnected
	require.True(t, result)
	start := time.Now()
	// Wait for reconnection
	result = <-reconnected
	elapsed := time.Since(start)
	assert.True(t, result)
	assert.True(t, s.client.IsConnected())
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), s.clientCfg.RetryBackOffWaitMinimum.Milliseconds())
	// Cleanup
	s.client.SetDisconnectedHandler(func(err error) {
		assert.Nil(t, err)
		clientOnDisconnected <- true
	})
	s.client.Stop()
	result = <-clientOnDisconnected
	require.True(t, result)
	s.server.Stop()
}

func (s *NetworkTestSuite) TestClientPongTimeout() {
	t := s.T()
	// Set timeouts for test
	// Will attempt to send ping after 1 second, and server expects ping within 1.4 seconds
	// Server will close connection
	s.clientCfg.PongWait = 2 * time.Second
	s.clientCfg.PingPeriod = (s.clientCfg.PongWait * 5) / 10
	// ocpp-go v0.19 has no ReconnectBackoff (its client retries through StartWithRetries); the
	// client side is upstream's code, so this test of its auto reconnect is not ported.
	t.Skip("client auto reconnect of ocpp-go v0.17 only")
	s.serverCfg.PingWait = (s.clientCfg.PongWait * 7) / 10
	// Setup
	serverOnDisconnected := make(chan bool, 1)
	clientOnDisconnected := make(chan bool, 1)
	reconnected := make(chan bool, 1)
	s.server.SetNewClientHandler(func(ws Channel) {
		assert.NotNil(t, ws)
	})
	s.server.SetDisconnectedClientHandler(func(ws Channel) {
		serverOnDisconnected <- true
	})
	s.server.SetMessageHandler(func(ws Channel, data []byte) error {
		assert.Fail(t, "unexpected message received")
		return fmt.Errorf("unexpected message received")
	})
	s.apply()
	go s.server.Start(serverPort, serverPath)
	time.Sleep(500 * time.Millisecond)

	// Test client
	s.client.SetDisconnectedHandler(func(err error) {
		defer func() {
			clientOnDisconnected <- true
		}()
		require.Error(t, err)
		closeError, ok := err.(*websocket.CloseError)
		require.True(t, ok)
		assert.Equal(t, websocket.CloseAbnormalClosure, closeError.Code)
	})
	s.client.SetReconnectedHandler(func() {
		time.Sleep(50 * time.Millisecond) // Make sure we reconnected after backoff
		reconnected <- true
	})
	host := s.proxy.Listen
	u := url.URL{Scheme: "ws", Host: host, Path: testPath}

	// Attempt connection
	err := s.client.Start(u.String())
	require.NoError(t, err)
	// Slow upstream network -> ping won't get through and server-side close will be triggered
	_, err = s.proxy.AddToxic("readTimeout", "timeout", "upstream", 1, toxiproxy.Attributes{
		"timeout": 5000, // 5 seconds
	})
	require.NoError(t, err)
	// Attempt to send message
	result := <-clientOnDisconnected
	require.True(t, result)
	result = <-serverOnDisconnected
	require.True(t, result)
	// Reconnect time starts
	_ = s.proxy.RemoveToxic("readTimeout")
	startTimeout := time.Now()
	result = <-reconnected
	require.True(t, result)
	elapsed := time.Since(startTimeout)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), s.clientCfg.RetryBackOffWaitMinimum.Milliseconds())
	// Cleanup
	s.client.SetDisconnectedHandler(nil)
	s.client.Stop()
	s.server.Stop()
}

func (s *NetworkTestSuite) TestClientReadTimeout() {
	t := s.T()
	// Set timeouts for test
	s.clientCfg.PongWait = 2 * time.Second
	s.clientCfg.PingPeriod = (s.clientCfg.PongWait * 7) / 10
	// ocpp-go v0.19 has no ReconnectBackoff (its client retries through StartWithRetries); the
	// client side is upstream's code, so this test of its auto reconnect is not ported.
	t.Skip("client auto reconnect of ocpp-go v0.17 only")
	s.serverCfg.PingWait = s.clientCfg.PongWait
	// Setup
	serverOnDisconnected := make(chan bool, 1)
	clientOnDisconnected := make(chan bool, 1)
	reconnected := make(chan bool, 1)
	s.server.SetNewClientHandler(func(ws Channel) {
		assert.NotNil(t, ws)
	})
	s.server.SetDisconnectedClientHandler(func(ws Channel) {
		serverOnDisconnected <- true
	})
	s.server.SetMessageHandler(func(ws Channel, data []byte) error {
		assert.Fail(t, "unexpected message received")
		return fmt.Errorf("unexpected message received")
	})
	s.apply()
	go s.server.Start(serverPort, serverPath)
	time.Sleep(500 * time.Millisecond)

	// Test client
	s.client.SetDisconnectedHandler(func(err error) {
		defer func() {
			clientOnDisconnected <- true
		}()
		require.Error(t, err)
		errMsg := err.Error()
		c := strings.Contains(errMsg, "timeout")
		if !c {
			fmt.Println(errMsg)
		}
		assert.True(t, c)
	})
	s.client.SetReconnectedHandler(func() {
		time.Sleep(50 * time.Millisecond) // Make sure we reconnected after backoff
		reconnected <- true
	})
	host := s.proxy.Listen
	u := url.URL{Scheme: "ws", Host: host, Path: testPath}

	// Attempt connection
	err := s.client.Start(u.String())
	require.NoError(t, err)
	// Slow down network. Ping will be received but pong won't go through
	_, err = s.proxy.AddToxic("writeTimeout", "timeout", "downstream", 1, toxiproxy.Attributes{
		"timeout": 5000, // 5 seconds
	})
	require.NoError(t, err)
	// Attempt to send message
	require.NoError(t, err)
	result := <-serverOnDisconnected
	require.True(t, result)
	result = <-clientOnDisconnected
	require.True(t, result)
	// Reconnect time starts
	s.proxy.RemoveToxic("writeTimeout")
	startTimeout := time.Now()
	result = <-reconnected
	require.True(t, result)
	elapsed := time.Since(startTimeout)
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), s.clientCfg.RetryBackOffWaitMinimum.Milliseconds())
	// Cleanup
	s.client.SetDisconnectedHandler(nil)
	s.client.Stop()
	s.server.Stop()
}

//TODO: test error channel from websocket

func TestNetworkErrors(t *testing.T) {
	suite.Run(t, new(NetworkTestSuite))
}
