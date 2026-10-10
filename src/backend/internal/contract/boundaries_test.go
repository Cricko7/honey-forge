package contract

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestNestedBinding(t *testing.T) {
	for _, body := range []string{`{"items":[{"port":null}]}`, `{"items":[{"Port":1}]}`, `{"child":{"port":null}}`} {
		t.Run(body, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.POST("/", func(c *gin.Context) {
				type child struct {
					Port int `json:"port"`
				}
				var in struct {
					Items []child `json:"items"`
					Child *child  `json:"child"`
				}
				if BindJSON(c, &in) {
					c.Status(200)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			if w.Code != 400 {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
}

func TestTrapPreconditions(t *testing.T) {
	for _, tt := range []struct {
		name   string
		values []string
		status int
	}{{"missing", nil, 428}, {"valid", []string{"1"}, 200}, {"overflow", []string{"2147483648"}, 400}, {"zero", []string{"0"}, 400}, {"duplicate", []string{"1", "1"}, 400}, {"negative", []string{"-1"}, 400}, {"comma", []string{"1,1"}, 400}} {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(Middleware())
			r.PATCH("/", func(c *gin.Context) {
				if _, ok := ExpectedTrapRevision(c); ok {
					c.Status(200)
				}
			})
			w := httptest.NewRecorder()
			req := httptest.NewRequest("PATCH", "/", nil)
			for _, v := range tt.values {
				req.Header.Add("X-Expected-Revision", v)
			}
			r.ServeHTTP(w, req)
			if w.Code != tt.status {
				t.Fatalf("status %d", w.Code)
			}
		})
	}
}

func TestSharedTypes(t *testing.T) {
	if _, err := NextRevision(MaxRevision); err == nil {
		t.Fatal("revision overflow accepted")
	}
	if r, err := NextRevision(1); err != nil || r != 2 {
		t.Fatal("revision increment")
	}
	if !ValidName("  ловушка  ") || ValidName("  ") || ValidName(strings.Repeat("я", 101)) {
		t.Fatal("name validation")
	}
	if err := CheckConfig(json.RawMessage(strings.Repeat("x", MaxConfigBytes+1))); err == nil {
		t.Fatal("oversized config accepted")
	}
	for _, s := range []string{"YQ", "YQ==\n", "YR=="} {
		if _, err := DecodeBytes(s, 1); err == nil {
			t.Fatalf("noncanonical base64 %q", s)
		}
	}
	if b, err := DecodeBytes("YQ==", 1); err != nil || string(b) != "a" {
		t.Fatal("base64")
	}
	b, err := json.Marshal(NewPage[int](nil, nil))
	if err != nil || string(b) != `{"items":[],"next_cursor":null}` {
		t.Fatalf("page %s %v", b, err)
	}
	v := Timestamp(time.Date(2026, 1, 1, 3, 0, 0, 0, time.FixedZone("MSK", 3*3600)))
	b, err = json.Marshal(v)
	if err != nil || string(b) != `"2026-01-01T00:00:00Z"` {
		t.Fatalf("timestamp %s %v", b, err)
	}
	for _, s := range []string{`"2026-01-01T00:00:00"`, `null`} {
		if err := json.Unmarshal([]byte(s), &v); err == nil {
			t.Fatalf("invalid timestamp %s", s)
		}
	}
}

func TestTimeRange(t *testing.T) {
	for _, tt := range []struct {
		name, from, to string
		code           string
	}{{"valid", "2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z", ""}, {"equal", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "invalid_time_range"}, {"no zone", "2026-01-01T00:00:00", "", "invalid_query"}} {
		t.Run(tt.name, func(t *testing.T) {
			v := url.Values{}
			if tt.from != "" {
				v.Set("from", tt.from)
			}
			if tt.to != "" {
				v.Set("to", tt.to)
			}
			_, e := ParseTimeRange(v)
			if tt.code == "" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil || e.Code != tt.code {
				t.Fatalf("error %v", e)
			}
		})
	}
}
