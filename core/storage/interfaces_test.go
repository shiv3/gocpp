package storage_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/storage"
)

func TestInterfacesExist(t *testing.T) {
	var (
		_ storage.ConnectionRegistry
		_ storage.MessageRouter
		_ storage.TransactionStore
		_ storage.ConfigStore
	)
}
