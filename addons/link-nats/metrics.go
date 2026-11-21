package linknats

import linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"

// Metrics is the link event sink; it is the same interface as the gRPC link's
// so one implementation serves both.
type Metrics = linkgrpc.Metrics

// NopMetrics ignores every event.
type NopMetrics = linkgrpc.NopMetrics
