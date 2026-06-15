package routerredis

import "time"

const (
	defaultChannelPrefix  = "gocpp:router"
	defaultRequestTimeout = 30 * time.Second
)

type config struct {
	channelPrefix  string
	requestTimeout time.Duration
	newID          func() string
}

func defaultConfig() config {
	return config{
		channelPrefix:  defaultChannelPrefix,
		requestTimeout: defaultRequestTimeout,
		newID:          randomID,
	}
}

// Option configures a Redis message router.
type Option func(*config)

// WithChannelPrefix sets the Redis channel prefix used for request and reply
// channels. The default is "gocpp:router".
func WithChannelPrefix(prefix string) Option {
	return func(c *config) {
		c.channelPrefix = prefix
	}
}

// WithRequestTimeout sets the maximum duration a CallRemote waits for the
// registry lookup, Redis subscribe/publish, and remote response. A zero or
// negative duration disables the router's timeout and relies only on ctx.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(c *config) {
		c.requestTimeout = timeout
	}
}

func withIDGenerator(fn func() string) Option {
	return func(c *config) {
		c.newID = fn
	}
}
