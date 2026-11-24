// Package txn is the transaction store of gocpp-csms serve mode: the
// transactions charge points report, per charge point and connector.
//
// OCPP 1.6 transactions get their id from the store (StartTransaction): a
// process-wide counter starting at 1. OCPP 2.x transactions carry the id the
// charge point chose (TransactionEvent transactionInfo.transactionId); the
// store keys them by charge point and that id. Every transaction also gets a
// store sequence number (Seq) in creation order.
package txn

import (
	"cmp"
	"slices"
	"strconv"
	"sync"
	"time"
)

// MaxEnded is the number of ended transactions kept for inspection.
const MaxEnded = 1000

// Transaction is one transaction as last reported by the charge point.
type Transaction struct {
	Seq           int64      `json:"seq"`
	CPID          string     `json:"cpId"`
	Version       string     `json:"version"`
	ID            string     `json:"transactionId"`
	EVSEID        int        `json:"evseId,omitempty"`
	ConnectorID   int        `json:"connectorId"`
	IDTag         string     `json:"idTag,omitempty"`
	MeterStart    *float64   `json:"meterStart,omitempty"`
	MeterStop     *float64   `json:"meterStop,omitempty"`
	ChargingState string     `json:"chargingState,omitempty"`
	StoppedReason string     `json:"stoppedReason,omitempty"`
	LastSeqNo     *int64     `json:"lastSeqNo,omitempty"`
	Started       time.Time  `json:"started"`
	Updated       time.Time  `json:"updated"`
	Ended         *time.Time `json:"ended,omitempty"`
	Active        bool       `json:"active"`
}

type key struct{ cp, id string }

// Store is safe for concurrent use.
type Store struct {
	mu     sync.Mutex
	nextID int64 // 1.6 transaction ids
	seq    int64
	active map[key]*Transaction
	ended  []Transaction // oldest first, at most MaxEnded
	now    func() time.Time
}

// New returns an empty store.
func New() *Store {
	return &Store{active: map[key]*Transaction{}, now: time.Now}
}

// Start16 records a 1.6 StartTransaction and returns the transaction with its
// new id. A transaction still active on the same connector is ended first
// (reason "Replaced").
func (s *Store) Start16(cpID string, connectorID int, idTag string, meterStart float64, ts time.Time) Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, t := range s.active {
		if k.cp == cpID && t.Version == "1.6" && t.ConnectorID == connectorID {
			s.endLocked(k, t, now, "Replaced")
		}
	}
	s.nextID++
	s.seq++
	ms := meterStart
	t := &Transaction{
		Seq:         s.seq,
		CPID:        cpID,
		Version:     "1.6",
		ID:          formatID(s.nextID),
		ConnectorID: connectorID,
		IDTag:       idTag,
		MeterStart:  &ms,
		Started:     orNow(ts, now),
		Updated:     now,
		Active:      true,
	}
	s.active[key{cpID, t.ID}] = t
	return *t
}

// Stop16 records a 1.6 StopTransaction. ok is false for an unknown or already
// ended transaction id.
func (s *Store) Stop16(cpID string, id int64, meterStop float64, ts time.Time, reason string) (Transaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key{cpID, formatID(id)}
	t, ok := s.active[k]
	if !ok {
		return Transaction{}, false
	}
	ms := meterStop
	t.MeterStop = &ms
	end := orNow(ts, s.now())
	if reason == "" {
		reason = "Local"
	}
	return s.endLocked(k, t, end, reason), true
}

// Touch16 marks a 1.6 transaction as updated (MeterValues with a
// transactionId). It reports whether the transaction is active.
func (s *Store) Touch16(cpID string, id int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.active[key{cpID, formatID(id)}]
	if ok {
		t.Updated = s.now()
	}
	return ok
}

// Event is a 2.x TransactionEvent as far as the store needs it.
type Event struct {
	Version       string
	EventType     string // Started, Updated, Ended
	TransactionID string
	Timestamp     time.Time
	SeqNo         *int64
	EVSEID        int
	ConnectorID   int
	IDToken       string
	ChargingState string
	StoppedReason string
	Meter         *float64 // Energy.Active.Import.Register in Wh, if reported
}

// Apply records a 2.x TransactionEvent. An Updated or Ended event for an
// unknown transaction creates it (the CSMS may have restarted).
func (s *Store) Apply(cpID string, ev Event) Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	k := key{cpID, ev.TransactionID}
	t, ok := s.active[k]
	if !ok {
		s.seq++
		t = &Transaction{
			Seq:     s.seq,
			CPID:    cpID,
			Version: ev.Version,
			ID:      ev.TransactionID,
			Started: orNow(ev.Timestamp, now),
			Active:  true,
		}
		if ev.Meter != nil {
			m := *ev.Meter
			t.MeterStart = &m
		}
		s.active[k] = t
	}
	t.Updated = now
	if ev.EVSEID != 0 {
		t.EVSEID = ev.EVSEID
	}
	if ev.ConnectorID != 0 {
		t.ConnectorID = ev.ConnectorID
	}
	if ev.IDToken != "" {
		t.IDTag = ev.IDToken
	}
	if ev.ChargingState != "" {
		t.ChargingState = ev.ChargingState
	}
	if ev.SeqNo != nil {
		n := *ev.SeqNo
		t.LastSeqNo = &n
	}
	if ev.EventType != "Ended" {
		return *t
	}
	if ev.Meter != nil {
		m := *ev.Meter
		t.MeterStop = &m
	}
	return s.endLocked(k, t, orNow(ev.Timestamp, now), ev.StoppedReason)
}

// endLocked moves an active transaction to the ended list.
func (s *Store) endLocked(k key, t *Transaction, at time.Time, reason string) Transaction {
	delete(s.active, k)
	t.Active = false
	t.Ended = &at
	t.Updated = s.now()
	if reason != "" {
		t.StoppedReason = reason
	}
	s.ended = append(s.ended, *t)
	if len(s.ended) > MaxEnded {
		s.ended = slices.Delete(s.ended, 0, len(s.ended)-MaxEnded)
	}
	return *t
}

// EndAll ends every active transaction of a charge point (for example after a
// reboot) and returns them.
func (s *Store) EndAll(cpID, reason string) []Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var out []Transaction
	for k, t := range s.active {
		if k.cp == cpID {
			out = append(out, s.endLocked(k, t, now, reason))
		}
	}
	sortBySeq(out)
	return out
}

// Get returns a transaction (active or among the kept ended ones).
func (s *Store) Get(cpID, id string) (Transaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.active[key{cpID, id}]; ok {
		return *t, true
	}
	for i := len(s.ended) - 1; i >= 0; i-- {
		if s.ended[i].CPID == cpID && s.ended[i].ID == id {
			return s.ended[i], true
		}
	}
	return Transaction{}, false
}

// Filter selects transactions in List.
type Filter struct {
	CPID       string // empty = every charge point
	ActiveOnly bool
}

// List returns the matching transactions ordered by Seq.
func (s *Store) List(f Filter) []Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Transaction{}
	for _, t := range s.active {
		if f.CPID == "" || t.CPID == f.CPID {
			out = append(out, *t)
		}
	}
	if !f.ActiveOnly {
		for _, t := range s.ended {
			if f.CPID == "" || t.CPID == f.CPID {
				out = append(out, t)
			}
		}
	}
	sortBySeq(out)
	return out
}

func sortBySeq(ts []Transaction) {
	slices.SortFunc(ts, func(a, b Transaction) int { return cmp.Compare(a.Seq, b.Seq) })
}

func orNow(ts, now time.Time) time.Time {
	if ts.IsZero() {
		return now
	}
	return ts
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
