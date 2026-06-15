package statefsm

import (
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

// TransactionBegin contains the storage fields recorded with StartTransaction.
type TransactionBegin struct {
	ID         string
	IDTag      string
	StartedAt  time.Time
	MeterStart int
	Metadata   map[string]any
}

// TransactionEnd contains the storage fields recorded with StopTransaction.
type TransactionEnd struct {
	ID        string
	EndedAt   time.Time
	MeterStop int
	Status    storage.TransactionStatus
}

func (b TransactionBegin) transaction(c *Connector) storage.Transaction {
	startedAt := b.StartedAt
	if startedAt.IsZero() {
		startedAt = c.clock()
	}

	return storage.Transaction{
		ID:         b.ID,
		CPID:       c.cpID,
		EVSEID:     c.connectorID,
		IDTag:      b.IDTag,
		StartedAt:  startedAt,
		MeterStart: b.MeterStart,
		Status:     storage.TransactionActive,
		Metadata:   cloneMetadata(b.Metadata),
	}
}

func (e TransactionEnd) transaction(c *Connector) storage.TransactionEnd {
	endedAt := e.EndedAt
	if endedAt.IsZero() {
		endedAt = c.clock()
	}

	status := e.Status
	if status == "" {
		status = storage.TransactionCompleted
	}

	return storage.TransactionEnd{
		EndedAt:   endedAt,
		MeterStop: e.MeterStop,
		Status:    status,
	}
}

func cloneMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
