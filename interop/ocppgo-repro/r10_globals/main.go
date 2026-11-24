// r10: configuration lives in package-level variables, so it is shared by
// every endpoint in the process and cannot be set per server or per fleet.
//
// Two central systems run in one process (two tenants, or a 1.6 and a 2.0.1
// listener). The operator of B wants to switch message validation off for
// B's fleet only. The API for it is the package function
// ocppj.SetMessageValidation. The program shows what that does to A, and
// whether it does what its documentation says for inbound messages at all.
package main

import (
	"fmt"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	a := h.StartCS(h.Opt{})
	b := h.StartCS(h.Opt{})
	fmt.Printf("library: %s\ntwo central systems in one process: A on :%d, B on :%d\n", h.Variant, a.Port, b.Port)
	cpA := h.MustDial(a.Port, "cpA")
	go func() {
		for {
			if _, raw, err := cpA.Read(time.Minute); err == nil {
				fmt.Println("   charge point of A received:", raw)
			} else {
				return
			}
		}
	}()
	cpB := h.MustDial(b.Port, "cpB")
	time.Sleep(200 * time.Millisecond)
	invalid := func() error { // idTag is required and limited to 20 characters
		return a.SendRequestAsync("cpA", core.NewRemoteStartTransactionRequest(""), func(ocpp.Response, error) {})
	}
	fmt.Println("1. A sends an invalid RemoteStartTransaction (empty idTag):", invalid())

	fmt.Println("2. operator of B calls ocppj.SetMessageValidation(false)  // meant for B's fleet")
	ocppj.SetMessageValidation(false)
	err := invalid()
	time.Sleep(200 * time.Millisecond)
	fmt.Println("3. A sends the same invalid request again: error =", err)
	coupled := err == nil

	_ = cpB.Send(`[2,"b1","BootNotification",{"chargePointModel":"m","chargePointVendor":""}]`)
	_, raw, _ := cpB.Read(2 * time.Second)
	fmt.Println("4. B's charge point sends BootNotification with an empty vendor (the quirk B wanted to tolerate); reply:", raw)
	stillRejected := len(raw) > 2 && raw[1] == '4'
	fmt.Printf("\nother process-wide settings of v0.19.0: ocppj.Validate (the validator instance, also aliased as ocpp1.6/types.Validate and ocpp2.0.1/types.Validate),\n" +
		"ocppj.SetLogger, ws.SetLogger, ocppj.SetHTMLEscape/EscapeHTML, ocppj.SetMessageIdGenerator, types.DateTimeFormat (one per OCPP version package).\n")
	h.Result(coupled || stillRejected, "setting changed for B also changed A=%v; inbound validation still on after SetMessageValidation(false)=%v", coupled, stillRejected)
}
