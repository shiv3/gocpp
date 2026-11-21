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
//
// Deprecated: storage.MessageRouter is superseded by gateway.Downstream; for new
// deployments use the gateway link (addons/link-grpc or addons/link-nats) instead
// of WithInstanceID plus a router. This package keeps working and is removed
// no earlier than v0.4. csms.RouterDownstream and csms.DownstreamRouter convert
// between the two while migrating (docs/clustering.md, "Moving to the gateway
// link").
package routernats
