// Package linknats is the NATS link between gateways and backends
// (docs/design/gateway.md 2.3): the same contract as addons/link-grpc
// (gocpp.link.v1 messages, Hello/HelloAck with protocol range and
// capabilities, Drain/DrainAck, Nack and Sent semantics, unknown frames
// answered Unsupported) carried by NATS core request-reply and queue groups.
//
// Subjects (P is the prefix, default "gocpp"; every dynamic part is a single
// token, dots and wildcard characters replaced by "_"):
//
//	P.in.<pool>.<version>.<action>   inbound CALL, queue group P.<pool>   (backends subscribe)
//	P.connect.<pool>                 upgrade decision, same queue group
//	P.notify.<pool>                  SEND and session events, same queue group
//	P.gw.<gatewayID>.call|send|control   CSMS-initiated work, one subscriber (the gateway)
//	P.hello / P.drain                backend announcements, every gateway listens
//	P.who                            "announce yourselves", every backend listens
//	P.cancel                         cancel hint for a DeliveryID
//
// A backend announces itself with Hello every interval (default 5 s); every
// gateway answers with HelloAck. That is the membership: a gateway lists the
// backends whose Hello is not older than three intervals, a backend lists the
// gateways whose HelloAck is. Because a queue group is chosen by the broker,
// each backend also sends an "accepted" frame to the request's reply subject
// the moment it receives a request; the gateway thereby learns who is working
// on it and fails the request (InternalError for the charger, never a
// redispatch) as soon as that backend's lease lapses instead of waiting for the
// deadline.
//
// Semantics that differ from the gRPC link, all inherent to NATS core: delivery
// is at most once, there is no flow control beyond the per-backend in-flight
// cap (excess requests are Nacked), and a dead peer is detected by the missed
// Hello rather than by a transport keepalive. "No responders" is the fast
// failure for "nobody is subscribed".
package linknats
