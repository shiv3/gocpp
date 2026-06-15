package statefsm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/looplab/fsm"
	"github.com/shiv3/gocpp/core/storage"
)

var (
	// ErrTransactionIDRequired is returned when a TransactionStore adapter is
	// configured but the transaction event does not include an ID.
	ErrTransactionIDRequired = errors.New("transaction ID required")
)

// Option configures a Connector.
type Option func(*options)

type options struct {
	initialState     State
	stateStore       StateStore
	transactionStore storage.TransactionStore
	clock            func() time.Time
}

// WithInitialState sets the connector's initial state when no stored state is
// available. Invalid states are ignored and Available is used instead.
func WithInitialState(state State) Option {
	return func(o *options) {
		o.initialState = state
	}
}

// WithStateStore sets the state persistence backend. A memory store is used by
// default.
func WithStateStore(store StateStore) Option {
	return func(o *options) {
		if store != nil {
			o.stateStore = store
		}
	}
}

// WithTransactionStore records StartTransaction and StopTransaction events in
// the supplied core transaction store.
func WithTransactionStore(store storage.TransactionStore) Option {
	return func(o *options) {
		o.transactionStore = store
	}
}

// WithClock sets the clock used for transaction timestamps when an event omits
// StartedAt or EndedAt.
func WithClock(clock func() time.Time) Option {
	return func(o *options) {
		if clock != nil {
			o.clock = clock
		}
	}
}

// Connector is an OCPP 1.6 connector state-machine wrapper.
type Connector struct {
	cpID        string
	connectorID int

	mu               sync.Mutex
	fsm              *fsm.FSM
	store            StateStore
	transactionStore storage.TransactionStore
	clock            func() time.Time
	loadErr          error
	activeTxID       string
}

// New returns a connector state-machine helper for a charge point connector.
func New(cpID string, connectorID int, opts ...Option) *Connector {
	cfg := options{
		initialState: StateAvailable,
		stateStore:   NewMemoryStateStore(),
		clock:        time.Now,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if !cfg.initialState.Valid() {
		cfg.initialState = StateAvailable
	}

	initial := cfg.initialState
	stored, ok, err := cfg.stateStore.Load(cpID, connectorID)
	if err == nil && ok && stored.Valid() {
		initial = stored
	}

	return &Connector{
		cpID:             cpID,
		connectorID:      connectorID,
		fsm:              newFSM(initial),
		store:            cfg.stateStore,
		transactionStore: cfg.transactionStore,
		clock:            cfg.clock,
		loadErr:          err,
	}
}

// CPID returns the charge point identifier associated with this connector.
func (c *Connector) CPID() string {
	return c.cpID
}

// ConnectorID returns the OCPP connector ID.
func (c *Connector) ConnectorID() int {
	return c.connectorID
}

// State returns the current OCPP connector status literal.
func (c *Connector) State() string {
	return c.fsm.Current()
}

// Can reports whether event is legal in the current state.
func (c *Connector) Can(event Event) bool {
	return c.fsm.Can(event.String())
}

// AvailableTransitions returns event names legal in the current state.
func (c *Connector) AvailableTransitions() []string {
	return c.fsm.AvailableTransitions()
}

// ActiveTransactionID returns the transaction ID last accepted by
// StartTransaction and not yet cleared by StopTransaction.
func (c *Connector) ActiveTransactionID() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeTxID == "" {
		return "", false
	}
	return c.activeTxID, true
}

// Fire runs a named event without transaction metadata.
func (c *Connector) Fire(event Event) error {
	return c.FireContext(context.Background(), event)
}

// FireContext runs a named event without transaction metadata.
func (c *Connector) FireContext(ctx context.Context, event Event) error {
	switch event {
	case EventStartTransaction:
		return c.StartTransactionContext(ctx, TransactionBegin{})
	case EventStopTransaction:
		return c.StopTransactionContext(ctx, TransactionEnd{})
	default:
		return c.transition(ctx, event)
	}
}

// PlugIn moves Available or Reserved connectors to Preparing. PlugIn is also
// legal while already Preparing.
func (c *Connector) PlugIn() error {
	return c.PlugInContext(context.Background())
}

// PlugInContext is the context-aware form of PlugIn.
func (c *Connector) PlugInContext(ctx context.Context) error {
	return c.transition(ctx, EventPlugIn)
}

// Authorize moves Available or Reserved connectors to Preparing. Authorize is
// also legal while already Preparing.
func (c *Connector) Authorize() error {
	return c.AuthorizeContext(context.Background())
}

// AuthorizeContext is the context-aware form of Authorize.
func (c *Connector) AuthorizeContext(ctx context.Context) error {
	return c.transition(ctx, EventAuthorize)
}

// StartTransaction moves Preparing connectors to Charging and optionally records
// the transaction in the configured TransactionStore.
func (c *Connector) StartTransaction(begin TransactionBegin) error {
	return c.StartTransactionContext(context.Background(), begin)
}

// StartTransactionContext is the context-aware form of StartTransaction.
func (c *Connector) StartTransactionContext(ctx context.Context, begin TransactionBegin) error {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ready(); err != nil {
		return err
	}
	if c.fsm.Cannot(EventStartTransaction.String()) {
		return c.fsm.Event(ctx, EventStartTransaction.String())
	}

	if c.transactionStore != nil {
		if begin.ID == "" {
			return ErrTransactionIDRequired
		}
		if err := c.transactionStore.Begin(ctx, begin.transaction(c)); err != nil {
			return fmt.Errorf("begin transaction: %w", err)
		}
	}
	if err := c.runLocked(ctx, EventStartTransaction); err != nil {
		return err
	}
	c.activeTxID = begin.ID
	return nil
}

// SuspendEV moves Charging or SuspendedEVSE connectors to SuspendedEV.
func (c *Connector) SuspendEV() error {
	return c.SuspendEVContext(context.Background())
}

// SuspendEVContext is the context-aware form of SuspendEV.
func (c *Connector) SuspendEVContext(ctx context.Context) error {
	return c.transition(ctx, EventSuspendEV)
}

// SuspendEVSE moves Charging or SuspendedEV connectors to SuspendedEVSE.
func (c *Connector) SuspendEVSE() error {
	return c.SuspendEVSEContext(context.Background())
}

// SuspendEVSEContext is the context-aware form of SuspendEVSE.
func (c *Connector) SuspendEVSEContext(ctx context.Context) error {
	return c.transition(ctx, EventSuspendEVSE)
}

// Resume moves SuspendedEV or SuspendedEVSE connectors back to Charging.
func (c *Connector) Resume() error {
	return c.ResumeContext(context.Background())
}

// ResumeContext is the context-aware form of Resume.
func (c *Connector) ResumeContext(ctx context.Context) error {
	return c.transition(ctx, EventResume)
}

// StopTransaction moves Charging, SuspendedEV, or SuspendedEVSE connectors to
// Finishing and optionally records the transaction end in the configured
// TransactionStore.
func (c *Connector) StopTransaction(end TransactionEnd) error {
	return c.StopTransactionContext(context.Background(), end)
}

// StopTransactionContext is the context-aware form of StopTransaction.
func (c *Connector) StopTransactionContext(ctx context.Context, end TransactionEnd) error {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ready(); err != nil {
		return err
	}
	if c.fsm.Cannot(EventStopTransaction.String()) {
		return c.fsm.Event(ctx, EventStopTransaction.String())
	}

	txID := end.ID
	if txID == "" {
		txID = c.activeTxID
	}
	if c.transactionStore != nil {
		if txID == "" {
			return ErrTransactionIDRequired
		}
		if err := c.transactionStore.End(ctx, txID, end.transaction(c)); err != nil {
			return fmt.Errorf("end transaction: %w", err)
		}
	}
	if err := c.runLocked(ctx, EventStopTransaction); err != nil {
		return err
	}
	if txID == c.activeTxID {
		c.activeTxID = ""
	}
	return nil
}

// Unplug moves Preparing or Finishing connectors to Available.
func (c *Connector) Unplug() error {
	return c.UnplugContext(context.Background())
}

// UnplugContext is the context-aware form of Unplug.
func (c *Connector) UnplugContext(ctx context.Context) error {
	return c.transition(ctx, EventUnplug)
}

// Reserve moves Available connectors to Reserved.
func (c *Connector) Reserve() error {
	return c.ReserveContext(context.Background())
}

// ReserveContext is the context-aware form of Reserve.
func (c *Connector) ReserveContext(ctx context.Context) error {
	return c.transition(ctx, EventReserve)
}

// CancelReservation moves Reserved connectors to Available.
func (c *Connector) CancelReservation() error {
	return c.CancelReservationContext(context.Background())
}

// CancelReservationContext is the context-aware form of CancelReservation.
func (c *Connector) CancelReservationContext(ctx context.Context) error {
	return c.transition(ctx, EventCancelReservation)
}

// Fault moves any non-Faulted connector to Faulted.
func (c *Connector) Fault() error {
	return c.FaultContext(context.Background())
}

// FaultContext is the context-aware form of Fault.
func (c *Connector) FaultContext(ctx context.Context) error {
	return c.transition(ctx, EventFault)
}

// ClearFault moves Faulted connectors to Available.
func (c *Connector) ClearFault() error {
	return c.ClearFaultContext(context.Background())
}

// ClearFaultContext is the context-aware form of ClearFault.
func (c *Connector) ClearFaultContext(ctx context.Context) error {
	return c.transition(ctx, EventClearFault)
}

// ChangeAvailability toggles between Available and Unavailable.
func (c *Connector) ChangeAvailability() error {
	return c.ChangeAvailabilityContext(context.Background())
}

// ChangeAvailabilityContext is the context-aware form of ChangeAvailability.
func (c *Connector) ChangeAvailabilityContext(ctx context.Context) error {
	return c.transition(ctx, EventChangeAvailability)
}

func (c *Connector) transition(ctx context.Context, event Event) error {
	if ctx == nil {
		ctx = context.Background()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ready(); err != nil {
		return err
	}
	return c.runLocked(ctx, event)
}

func (c *Connector) ready() error {
	if c.loadErr != nil {
		return fmt.Errorf("load connector state: %w", c.loadErr)
	}
	return nil
}

func (c *Connector) runLocked(ctx context.Context, event Event) error {
	if err := c.fsm.Event(ctx, event.String()); err != nil {
		var noTransition fsm.NoTransitionError
		if !errors.As(err, &noTransition) {
			return err
		}
	}
	if err := c.store.Save(c.cpID, c.connectorID, State(c.fsm.Current())); err != nil {
		return fmt.Errorf("save connector state: %w", err)
	}
	return nil
}

func newFSM(initial State) *fsm.FSM {
	return fsm.NewFSM(initial.String(), fsm.Events{
		{Name: EventPlugIn.String(), Src: states(StateAvailable, StateReserved), Dst: StatePreparing.String()},
		{Name: EventPlugIn.String(), Src: states(StatePreparing), Dst: StatePreparing.String()},
		{Name: EventAuthorize.String(), Src: states(StateAvailable, StateReserved), Dst: StatePreparing.String()},
		{Name: EventAuthorize.String(), Src: states(StatePreparing), Dst: StatePreparing.String()},
		{Name: EventStartTransaction.String(), Src: states(StatePreparing), Dst: StateCharging.String()},
		{Name: EventSuspendEV.String(), Src: states(StateCharging, StateSuspendedEVSE), Dst: StateSuspendedEV.String()},
		{Name: EventSuspendEVSE.String(), Src: states(StateCharging, StateSuspendedEV), Dst: StateSuspendedEVSE.String()},
		{Name: EventResume.String(), Src: states(StateSuspendedEV, StateSuspendedEVSE), Dst: StateCharging.String()},
		{Name: EventStopTransaction.String(), Src: states(StateCharging, StateSuspendedEV, StateSuspendedEVSE), Dst: StateFinishing.String()},
		{Name: EventUnplug.String(), Src: states(StatePreparing, StateFinishing), Dst: StateAvailable.String()},
		{Name: EventReserve.String(), Src: states(StateAvailable), Dst: StateReserved.String()},
		{Name: EventCancelReservation.String(), Src: states(StateReserved), Dst: StateAvailable.String()},
		{Name: EventFault.String(), Src: states(StateAvailable, StatePreparing, StateCharging, StateSuspendedEV, StateSuspendedEVSE, StateFinishing, StateReserved, StateUnavailable), Dst: StateFaulted.String()},
		{Name: EventClearFault.String(), Src: states(StateFaulted), Dst: StateAvailable.String()},
		{Name: EventChangeAvailability.String(), Src: states(StateAvailable), Dst: StateUnavailable.String()},
		{Name: EventChangeAvailability.String(), Src: states(StateUnavailable), Dst: StateAvailable.String()},
	}, fsm.Callbacks{})
}

func states(states ...State) []string {
	names := make([]string, len(states))
	for i, state := range states {
		names[i] = state.String()
	}
	return names
}
