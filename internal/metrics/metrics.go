package metrics

import (
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

func RecordAuthRequest(authType, status string) {
	AuthRequestsTotal.WithLabelValues(authType, status).Inc()
}

func RecordGRPCCall(method, status string) {
	GRPCCallsTotal.WithLabelValues(method, status).Inc()
}
