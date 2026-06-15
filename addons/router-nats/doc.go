// Package routernats implements storage.MessageRouter over NATS request/reply.
//
// Each CSMS instance serves requests on a subject derived from its instance ID.
// CallRemote resolves the target instance through storage.ConnectionRegistry and
// sends a NATS request to that instance's subject. The remote instance invokes
// the storage.RemoteHandler it was given by ServeRemote and replies with the
// handler result.
//
// Typical setup:
//
//	nc, _ := nats.Connect(nats.DefaultURL)
//	router := routernats.New(nc, "csms-a", registry)
//	server := csms.NewServer(csms.WithMessageRouter(router))
package routernats
