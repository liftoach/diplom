package control

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRejectsInvalidRequestsBeforeDatabase(t *testing.T) {
	api := &API{}
	handler := api.Mux()
	cases := []struct{ method, path, body string }{
		{"POST", "/api/v1/devices", `{"id":"bad/+","name":"test","expected_interval_seconds":1}`},
		{"POST", "/api/v1/devices", `{"id":"valid","name":"test","expected_interval_seconds":0}`},
		{"POST", "/api/v1/devices", `{"id":"valid","name":"test","expected_interval_seconds":1} {}`},
		{"POST", "/api/v1/devices", strings.Repeat("x", 5000)},
		{"GET", "/api/v1/devices?limit=1001", ""},
		{"GET", "/api/v1/devices?after=bad!", ""},
		{"GET", "/api/v1/devices/valid/measurements?from=nope", ""},
		{"GET", "/api/v1/devices/valid/measurements?from=2026-01-01T00:00:00Z&to=2026-02-01T00:00:00Z", ""},
		{"GET", "/api/v1/devices/valid/measurements?before=2026-01-01T00:00:00Z", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path+tc.body, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 400 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["code"] == "" || body["request_id"] == "" {
				t.Fatal("missing error contract")
			}
		})
	}
}
