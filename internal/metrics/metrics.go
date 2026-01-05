package metrics

import (
	"runtime"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// WebSocket connections (gauge - goes up and down)
	WebSocketConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "websocket_connections",
		Help: "Current number of WebSocket connections",
	})

	// Messages sent (counter - only goes up)
	MessagesSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "messages_sent_total",
		Help: "Total number of messages sent",
	}, []string{"room_type"})

	// Messages received (counter)
	MessagesReceivedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "messages_received_total",
		Help: "Total number of messages received",
	})

	// Auth requests (counter)
	AuthRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "auth_requests_total",
		Help: "Total authentication requests",
	}, []string{"type", "status"})

	// gRPC calls to User Service (counter)
	GRPCCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "grpc_calls_total",
		Help: "Total gRPC calls to User Service",
	}, []string{"method", "status"})

	// === NEW METRICS ===

	// Message latency (histogram)
	MessageLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "message_latency_ms",
		Help:    "Message processing latency in milliseconds",
		Buckets: []float64{1, 2, 5, 10, 25, 50, 100, 250, 500, 1000},
	})

	// Active rooms (gauge)
	ActiveRooms = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "active_rooms",
		Help: "Number of active chat rooms",
	})

	// Users per room (histogram)
	UsersPerRoom = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "users_per_room",
		Help:    "Distribution of users per room",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 500, 1000, 5000},
	})

	// Goroutines count (gauge)
	GoroutinesCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "goroutines_count",
		Help: "Current number of goroutines",
	})

	// Connection duration (histogram)
	ConnectionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "connection_duration_seconds",
		Help:    "WebSocket connection duration in seconds",
		Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800, 3600},
	})

	// Broadcast duration (histogram)
	BroadcastDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "broadcast_duration_ms",
		Help:    "Time to broadcast message to room in milliseconds",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 25, 50, 100},
	})

	// Database query duration (histogram)
	DBQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "db_query_duration_ms",
		Help:    "Database query duration in milliseconds",
		Buckets: []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
	}, []string{"operation"})

	// Redis operation duration (histogram)
	RedisOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "redis_operation_duration_ms",
		Help:    "Redis operation duration in milliseconds",
		Buckets: []float64{0.5, 1, 2, 5, 10, 25, 50, 100},
	}, []string{"operation"})

	// Dropped messages (counter)
	DroppedMessages = promauto.NewCounter(prometheus.CounterOpts{
		Name: "dropped_messages_total",
		Help: "Total number of dropped messages due to full buffer",
	})

	// Room joins (counter)
	RoomJoinsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "room_joins_total",
		Help: "Total number of room joins",
	})

	// Room leaves (counter)
	RoomLeavesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "room_leaves_total",
		Help: "Total number of room leaves",
	})
)

// Helper functions

func IncWebSocketConnection() {
	WebSocketConnections.Inc()
}

func DecWebSocketConnection() {
	WebSocketConnections.Dec()
}

func RecordMessageSent(roomType string) {
	MessagesSentTotal.WithLabelValues(roomType).Inc()
}

func RecordMessageReceived() {
	MessagesReceivedTotal.Inc()
}

func RecordAuthRequest(authType, status string) {
	AuthRequestsTotal.WithLabelValues(authType, status).Inc()
}

func RecordGRPCCall(method, status string) {
	GRPCCallsTotal.WithLabelValues(method, status).Inc()
}

// RecordMessageLatency records the time taken to process a message
func RecordMessageLatency(startTime time.Time) {
	MessageLatency.Observe(float64(time.Since(startTime).Milliseconds()))
}

// SetActiveRooms sets the current number of active rooms
func SetActiveRooms(count int) {
	ActiveRooms.Set(float64(count))
}

// RecordUsersPerRoom records the number of users in a room
func RecordUsersPerRoom(count int) {
	UsersPerRoom.Observe(float64(count))
}

// UpdateGoroutinesCount updates the goroutines gauge
func UpdateGoroutinesCount() {
	GoroutinesCount.Set(float64(runtime.NumGoroutine()))
}

// RecordConnectionDuration records how long a connection was active
func RecordConnectionDuration(startTime time.Time) {
	ConnectionDuration.Observe(time.Since(startTime).Seconds())
}

// RecordBroadcastDuration records the time taken to broadcast a message
func RecordBroadcastDuration(startTime time.Time) {
	BroadcastDuration.Observe(float64(time.Since(startTime).Milliseconds()))
}

// RecordDBQuery records database query duration
func RecordDBQuery(operation string, startTime time.Time) {
	DBQueryDuration.WithLabelValues(operation).Observe(float64(time.Since(startTime).Milliseconds()))
}

// RecordRedisOperation records Redis operation duration
func RecordRedisOperation(operation string, startTime time.Time) {
	RedisOperationDuration.WithLabelValues(operation).Observe(float64(time.Since(startTime).Milliseconds()))
}

// RecordDroppedMessage records a dropped message
func RecordDroppedMessage() {
	DroppedMessages.Inc()
}

// RecordRoomJoin records a room join
func RecordRoomJoin() {
	RoomJoinsTotal.Inc()
}

// RecordRoomLeave records a room leave
func RecordRoomLeave() {
	RoomLeavesTotal.Inc()
}

// StartMetricsUpdater starts a goroutine that periodically updates system metrics
func StartMetricsUpdater() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			UpdateGoroutinesCount()
		}
	}()
}
