package ntfy

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// The hub defaults.
const (
	// DefaultHeartbeat is how often an idle stream is written to, so that
	// proxies and load balancers do not close it.
	DefaultHeartbeat = 25 * time.Second
	// DefaultWriteTimeout is how long a stream waits for a client to accept a
	// write before closing the stream.
	DefaultWriteTimeout = 10 * time.Second
	// DefaultMaxStreamsPerRecipient is how many streams one recipient may hold
	// open on one instance, across every transport.
	DefaultMaxStreamsPerRecipient = 8
	// DefaultReconnectDelay is the base a stream tells its client to wait before
	// reconnecting. The value a stream carries is drawn between it and twice it.
	DefaultReconnectDelay = time.Second
)

// Hub routes signals from a [Broadcaster] to the subscriptions of the recipient
// each signal is for. Every transport, SSE and WebSocket alike, subscribes
// through it, so one per-recipient cap counts all of a recipient's streams.
//
// Nothing runs on its own: signals are received only while the host runs
// [Hub.Run]. A Hub is safe for concurrent use.
type Hub struct {
	broadcaster  Broadcaster
	heartbeat    time.Duration
	writeTimeout time.Duration
	reconnect    time.Duration
	maxStreams   int

	// running is true only while a run's broadcaster has confirmed its
	// subscription. It is written under runMu and read without it.
	running atomic.Bool

	// runMu guards the run state: the run in progress, if any, and the channel
	// Ready hands out.
	runMu   sync.Mutex
	current *hubRun
	readyCh chan struct{}

	mu sync.Mutex
	// receiving is the subscription side's view of running. It is written under
	// mu, so that Subscribe decides and inserts in one critical section and a run
	// ending in between cannot leave an orphaned subscription behind.
	receiving     bool
	subscriptions map[string]map[*Subscription]struct{}
}

// HubOption configures a [Hub].
type HubOption func(*hubConfig)

// hubConfig is what the options set. The values are pointers because an explicit
// zero is a wiring mistake to report, while an absent value is the default.
type hubConfig struct {
	heartbeat    *time.Duration
	writeTimeout *time.Duration
	reconnect    *time.Duration
	maxStreams   *int
}

// WithHeartbeat replaces [DefaultHeartbeat]. It must be positive.
func WithHeartbeat(interval time.Duration) HubOption {
	return func(c *hubConfig) { c.heartbeat = &interval }
}

// WithWriteTimeout replaces [DefaultWriteTimeout]. It must be positive.
func WithWriteTimeout(timeout time.Duration) HubOption {
	return func(c *hubConfig) { c.writeTimeout = &timeout }
}

// WithMaxStreamsPerRecipient replaces [DefaultMaxStreamsPerRecipient]. It must be
// at least one.
func WithMaxStreamsPerRecipient(n int) HubOption {
	return func(c *hubConfig) { c.maxStreams = &n }
}

// WithReconnectDelay replaces [DefaultReconnectDelay], the base a stream tells
// its client to wait before reconnecting. It must be positive.
//
// The jitter that spreads it is not configurable: a base with no spread returns
// an instance's clients in one wave, which is what the delay exists to prevent.
func WithReconnectDelay(d time.Duration) HubOption {
	return func(c *hubConfig) { c.reconnect = &d }
}

// NewHub builds a hub over a broadcaster, normally the service's
// [Service.Broadcaster]. A nil broadcaster, or a heartbeat, write timeout or
// stream cap that is not positive, is a [ConfigurationError].
func NewHub(broadcaster Broadcaster, opts ...HubOption) (*Hub, error) {
	if broadcaster == nil {
		return nil, &ConfigurationError{Detail: "a hub needs a broadcaster; pass the service's Broadcaster()"}
	}

	var cfg hubConfig

	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	hub := &Hub{
		broadcaster:   broadcaster,
		heartbeat:     DefaultHeartbeat,
		writeTimeout:  DefaultWriteTimeout,
		reconnect:     DefaultReconnectDelay,
		maxStreams:    DefaultMaxStreamsPerRecipient,
		readyCh:       make(chan struct{}),
		subscriptions: make(map[string]map[*Subscription]struct{}),
	}

	switch {
	case cfg.heartbeat != nil && *cfg.heartbeat <= 0:
		return nil, &ConfigurationError{Detail: "a hub heartbeat must be positive"}
	case cfg.writeTimeout != nil && *cfg.writeTimeout <= 0:
		return nil, &ConfigurationError{Detail: "a hub write timeout must be positive"}
	case cfg.reconnect != nil && *cfg.reconnect <= 0:
		return nil, &ConfigurationError{Detail: "a hub reconnect delay must be positive"}
	case cfg.maxStreams != nil && *cfg.maxStreams < 1:
		return nil, &ConfigurationError{Detail: "a hub must allow at least one stream per recipient"}
	}

	if cfg.heartbeat != nil {
		hub.heartbeat = *cfg.heartbeat
	}

	if cfg.writeTimeout != nil {
		hub.writeTimeout = *cfg.writeTimeout
	}

	if cfg.reconnect != nil {
		hub.reconnect = *cfg.reconnect
	}

	if cfg.maxStreams != nil {
		hub.maxStreams = *cfg.maxStreams
	}

	return hub, nil
}

// Run receives signals from the broadcaster until ctx is cancelled, and returns
// ctx's error, or the broadcaster's error when it could not subscribe.
//
// A run starts unready: the hub refuses streams until the broadcaster confirms
// its subscription, and only then does [Hub.Running] report true and
// [Hub.Ready] close. A broadcaster that never confirms leaves the hub refusing
// every stream as unavailable.
//
// It blocks, and a hub runs once at a time: a second Run while one is starting
// or running is a [ConfigurationError]. A hub whose Run has returned may be run
// again.
func (h *Hub) Run(ctx context.Context) error {
	h.runMu.Lock()

	if h.current != nil {
		h.runMu.Unlock()

		return &ConfigurationError{Detail: "the hub is already running; run it once"}
	}

	run := &hubRun{}
	h.current = run

	h.runMu.Unlock()

	defer h.endRun()

	return h.broadcaster.Listen(ctx, h.deliver, func() { h.markReady(run) })
}

// hubRun identifies one [Hub.Run] by its address. It has a field because
// pointers to distinct zero-size values may compare equal.
type hubRun struct{ _ byte }

// markReady records that a run's broadcaster has subscribed. It changes nothing
// for a run that is not the current one, such as one that has ended, or when the
// current run is already receiving, so a late or repeated ready is harmless.
func (h *Hub) markReady(run *hubRun) {
	h.runMu.Lock()
	defer h.runMu.Unlock()

	if h.current != run || h.running.Load() {
		return
	}

	h.running.Store(true)

	h.mu.Lock()
	h.receiving = true
	h.mu.Unlock()

	close(h.readyCh)
}

// endRun returns the hub to idle. A Ready channel the run closed is replaced by
// an open one for the next run; one it never closed is kept, so that a host
// still waiting on it is woken by whichever run subscribes next.
func (h *Hub) endRun() {
	h.runMu.Lock()
	defer h.runMu.Unlock()

	h.current = nil
	h.running.Store(false)
	h.closeSubscriptions()

	select {
	case <-h.readyCh:
		h.readyCh = make(chan struct{})
	default:
	}
}

// closeSubscriptions stops accepting streams and closes every open one, so that
// no stream outlives the run that fed it. A later run starts with none.
func (h *Hub) closeSubscriptions() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.receiving = false

	for _, held := range h.subscriptions {
		for subscription := range held {
			subscription.closeLocked()
		}
	}

	h.subscriptions = make(map[string]map[*Subscription]struct{})
}

// Running reports whether the hub is receiving signals: a run is in progress and
// its broadcaster has confirmed its subscription, so a signal broadcast now
// reaches this instance's streams, within the broadcaster's best effort.
//
// After a broker connection drops, the broadcaster's client resubscribes on its
// own and Running stays true meanwhile; signals in that gap are lost, which the
// best-effort contract allows.
func (h *Hub) Running() bool { return h.running.Load() }

// Ready returns a channel that is closed once the run in progress, or the next
// run when none is, has its broadcaster's subscription confirmed. It lets a host
// or a test wait for the hub instead of polling [Hub.Running]:
//
//	runErr := make(chan error, 1)
//	go func() { runErr <- hub.Run(ctx) }()
//
//	select {
//	case <-hub.Ready():
//		// streams are accepted from here
//	case err := <-runErr:
//		// the broadcaster could not subscribe; Ready will not close for this run
//	}
//
// A receive says the hub became ready at some point after the call, not that it
// still is: a channel held across a stop stays closed, and [Hub.Running] gives
// the current answer. A run that fails before its broadcaster is ready never
// closes the channel.
func (h *Hub) Ready() <-chan struct{} {
	h.runMu.Lock()
	defer h.runMu.Unlock()

	return h.readyCh
}

// Heartbeat is how often a transport writes to an idle stream.
func (h *Hub) Heartbeat() time.Duration { return h.heartbeat }

// WriteTimeout is how long a transport waits for a client to accept a write.
func (h *Hub) WriteTimeout() time.Duration { return h.writeTimeout }

// ReconnectDelay is how long a new stream tells its client to wait before
// reconnecting. Every call draws its own value, between the configured base —
// [DefaultReconnectDelay] unless [WithReconnectDelay] replaces it — and twice
// it, so that an instance's clients do not all return at the same moment.
func (h *Hub) ReconnectDelay() time.Duration {
	return h.reconnect + time.Duration(rand.Int64N(int64(h.reconnect)))
}

// deliver offers a signal to every subscription of its recipient. It never
// waits on a client.
func (h *Hub) deliver(signal Signal) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for subscription := range h.subscriptions[signal.Recipient] {
		subscription.offer(signal)
	}
}

// Subscribe opens a subscription to a recipient's signals.
//
// It does not authorize: a transport asks its [SubscriptionAuthorizer] first.
// A hub that is not receiving signals refuses with an error matching
// [ErrUnavailable], and a recipient already holding the maximum number of
// subscriptions on this instance is refused with one matching
// [ErrTooManyStreams]. Every subscription must be closed, and a subscription is
// closed for the caller when the hub's run ends.
func (h *Hub) Subscribe(recipient string) (*Subscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.receiving {
		return nil, fmt.Errorf("%w: the hub is not receiving signals", ErrUnavailable)
	}

	held := h.subscriptions[recipient]
	if len(held) >= h.maxStreams {
		return nil, fmt.Errorf("%w: %d streams are already open for this recipient", ErrTooManyStreams, h.maxStreams)
	}

	if held == nil {
		held = make(map[*Subscription]struct{})
		h.subscriptions[recipient] = held
	}

	subscription := &Subscription{
		hub:       h,
		recipient: recipient,
		ready:     make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	held[subscription] = struct{}{}

	return subscription, nil
}

// Subscription is one open stream's view of a recipient's signals.
//
// It holds at most one pending signal: signals arriving before the stream takes
// one replace it. A signal carries no content, so the latest says everything
// the ones it replaced did, and a client that stops reading costs no memory and
// never slows a publisher.
type Subscription struct {
	hub       *Hub
	recipient string
	ready     chan struct{}
	done      chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	pending Signal
	has     bool
}

// offer makes a signal the pending one and wakes the reader, without waiting.
func (s *Subscription) offer(signal Signal) {
	s.mu.Lock()
	s.pending = signal
	s.has = true
	s.mu.Unlock()

	select {
	case s.ready <- struct{}{}:
	default:
	}
}

// Ready receives when a signal may be pending. A receive can find nothing to
// take, which is harmless; [Subscription.Take] says for certain.
func (s *Subscription) Ready() <-chan struct{} { return s.ready }

// Take returns the pending signal and clears it, reporting false when none is
// pending.
func (s *Subscription) Take() (Signal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.has {
		return Signal{}, false
	}

	signal := s.pending
	s.pending, s.has = Signal{}, false

	return signal, true
}

// Done is closed when the subscription ends, whether its stream closed it or
// its instance stopped receiving signals. A transport selects on it alongside
// [Subscription.Ready] and ends the stream when it closes; a transport that
// ignores it is left holding a stream no signal will ever reach.
func (s *Subscription) Done() <-chan struct{} { return s.done }

// Close ends the subscription and frees its slot in the recipient's cap. It is
// safe to call more than once.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()

	s.closeLocked()
}

// closeLocked ends the subscription exactly once. The caller holds the hub's
// lock, so that a stop can close every subscription in one critical section.
func (s *Subscription) closeLocked() {
	s.closeOnce.Do(func() {
		close(s.done)

		held := s.hub.subscriptions[s.recipient]
		if _, open := held[s]; !open {
			return
		}

		delete(held, s)

		if len(held) == 0 {
			delete(s.hub.subscriptions, s.recipient)
		}
	})
}
