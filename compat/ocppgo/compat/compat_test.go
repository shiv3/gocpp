// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package compat_test

import (
	"os"
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/compat"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

func TestRuntimeSelection(t *testing.T) {
	legacy := os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy"
	s := ocppj.NewServer(ws.NewServer(), nil, nil)
	want := "gocpp"
	if legacy {
		want = "legacy"
	}
	if got := compat.Runtime(s); got != want {
		t.Fatalf("runtime %q, want %q", got, want)
	}
	if ok := compat.SetCallTimeout(s, time.Second); ok == legacy {
		t.Fatalf("SetCallTimeout = %v", ok)
	}
	// An application that injects its own dispatcher keeps ocpp-go's code.
	d := ocppj.NewDefaultServerDispatcher(ocppj.NewFIFOQueueMap(0))
	injected := ocppj.NewServer(ws.NewServer(), d, nil)
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "" && compat.Runtime(injected) != "legacy" {
		t.Fatal("an injected dispatcher must select the original runtime")
	}
}
