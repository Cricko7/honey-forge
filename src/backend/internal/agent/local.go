package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gorilla/websocket"

	"honey-forge/internal/configschema"
	"honey-forge/internal/contract"
	"honey-forge/internal/platform/httpx"
	"honey-forge/internal/platform/traffic"
	"honey-forge/modules/catalog"
	"honey-forge/modules/events"
	"honey-forge/modules/profiles"
)

type Local struct {
	Traffic         chan traffic.Frame
	TrafficDropped  atomic.Uint64
	ReceivedTraffic *traffic.Inbox
	Journal         *Journal
	Token           string
	Ready           chan struct{}
	Failures        chan string
	mu              sync.Mutex
	snapshot        profiles.Snapshot
	schemas         map[eventSchemaKey]*configschema.Schema
}

type eventSchemaKey struct {
	typeID  string
	version int64
	event   string
}

func NewLocal(j *Journal, token string) (*Local, error) {
	l := &Local{Journal: j, Token: token, Ready: make(chan struct{}, 1), Failures: make(chan string, 1), schemas: map[eventSchemaKey]*configschema.Schema{}}
	l.Traffic = make(chan traffic.Frame, traffic.QueueCapacity)
	l.ReceivedTraffic = traffic.NewInbox(traffic.QueueCapacity)
	for _, definition := range catalog.BuiltinDefinitions() {
		for _, e := range definition.Entry.EventSchemas {
			schema, err := configschema.Compile(e.DataSchema)
			if err != nil {
				return nil, err
			}
			l.schemas[eventSchemaKey{string(definition.Entry.TypeID), int64(definition.Entry.TypeVersion), string(e.EventType)}] = schema
		}
	}
	contract.Configure()
	return l, nil
}

func (l *Local) Configure(snapshot profiles.Snapshot) {
	l.mu.Lock()
	l.snapshot = snapshot
	l.mu.Unlock()
	select {
	case <-l.Ready:
	default:
	}
	select {
	case <-l.Failures:
	default:
	}
}

func (l *Local) Handler(ctx context.Context) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/trap-stream", func(c *gin.Context) {
		if c.Request.TLS == nil {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "forbidden", "message": "Local TLS connection required"}})
			return
		}
		if c.Request.URL.RawQuery != "" || len(c.Request.Header.Values("Origin")) > 0 {
			c.JSON(http.StatusForbidden, gin.H{"error": gin.H{"code": "forbidden", "message": "Local agent connection required"}})
			return
		}
		if len(c.Request.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare([]byte(c.GetHeader("Authorization")), []byte("Bearer "+l.Token)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "unauthenticated", "message": "Authentication required"}})
			return
		}
		upgrader := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		cancel := context.AfterFunc(ctx, func() { conn.Close() })
		defer cancel()
		conn.SetReadLimit(16 * 1024)
		for {
			kind, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if kind != websocket.TextMessage {
				conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1003, "unsupported_data"), time.Now().Add(time.Second))
				return
			}
			var notice struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw, &notice) == nil && notice.Type == "trap.ready" {
				select {
				case l.Ready <- struct{}{}:
				default:
				}
				if err := writeLocal(conn, map[string]any{"ready": true}); err != nil {
					return
				}
				continue
			}
			if notice.Type == "trap.error" {
				select {
				case l.Failures <- "buffer_unavailable":
				default:
				}
				return
			}
			if notice.Type == "trap.traffic" {
				var body struct {
					Type  string        `json:"type"`
					Frame traffic.Frame `json:"frame"`
				}
				valid := contract.CheckJSON(raw) == nil && httpx.StrictObject(raw, reflect.TypeOf(body)) && json.Unmarshal(raw, &body) == nil
				if !valid || l.acceptTraffic(body.Frame) != nil {
					if err := writeLocal(conn, map[string]any{"error": map[string]string{"code": "validation_failed"}}); err != nil {
						return
					}
					continue
				}
				if err := writeLocal(conn, map[string]any{"traffic_connection_id": body.Frame.ConnectionID}); err != nil {
					return
				}
				continue
			}
			var e events.AgentEvent
			l.mu.Lock()
			snapshot := l.snapshot
			l.mu.Unlock()
			var schema *configschema.Schema
			valid := contract.CheckJSON(raw) == nil && httpx.StrictObject(raw, reflect.TypeFor[events.AgentEvent]()) && json.Unmarshal(raw, &e) == nil && binding.Validator.ValidateStruct(e) == nil
			if valid {
				schema = l.schemas[eventSchemaKey{e.TypeID, e.TypeVersion, e.EventType}]
			}
			valid = valid && schema != nil && schema.Validate(e.Data, "") == nil && e.TypeID == snapshot.TypeID && e.TypeVersion == int64(snapshot.TypeVersion) && e.ProfileRevision == int64(snapshot.ProfileRevision)
			if !valid {
				writeLocal(conn, map[string]any{"error": map[string]string{"code": "telemetry_invalid", "message": "Telemetry event is invalid"}})
				return
			}
			if _, err := l.Journal.Enqueue(e); err != nil {
				code := "buffer_unavailable"
				if errors.Is(err, ErrBufferFull) {
					code = "buffer_full"
				}
				select {
				case l.Failures <- code:
				default:
				}
				writeLocal(conn, map[string]any{"error": map[string]string{"code": code, "message": "Telemetry buffer cannot accept event"}})
				return
			}
			if err := writeLocal(conn, map[string]any{"event_id": e.EventID}); err != nil {
				return
			}
		}
	})
	return router
}

func writeLocal(conn *websocket.Conn, payload any) error {
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	return conn.WriteJSON(payload)
}
