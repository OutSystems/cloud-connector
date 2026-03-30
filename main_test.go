package main

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"
)

func Test_emitObsEvent(t *testing.T) {
	const testCorrelationID = "550e8400-e29b-41d4-a716-446655440000"
	tests := []struct {
		name          string
		eventType     string
		status        string
		server        string
		remotes       []string
		destHosts     []string
		latencyMs     *int64
		obsErr        *string
		wantEventType string
		wantStatus    string
		wantLatency   bool // true = expect non-null latency_ms
		wantErr       bool // true = expect non-null error
	}{
		{
			name:          "tunnel_starting no latency no error",
			eventType:     "tunnel_starting",
			status:        "starting",
			server:        "wss://pg.example.com",
			remotes:       []string{"R:8081:db.internal:5432"},
			destHosts:     []string{"db.internal"},
			latencyMs:     nil,
			obsErr:        nil,
			wantEventType: "tunnel_starting",
			wantStatus:    "starting",
			wantLatency:   false,
			wantErr:       false,
		},
		{
			name:          "tunnel_connected with latency",
			eventType:     "tunnel_connected",
			status:        "connected",
			server:        "wss://pg.example.com",
			remotes:       []string{"R:8081:db.internal:5432"},
			destHosts:     []string{"db.internal"},
			latencyMs:     func() *int64 { v := int64(266); return &v }(),
			obsErr:        nil,
			wantEventType: "tunnel_connected",
			wantStatus:    "connected",
			wantLatency:   true,
			wantErr:       false,
		},
		{
			name:          "tunnel_error with error string",
			eventType:     "tunnel_error",
			status:        "error",
			server:        "wss://pg.example.com",
			remotes:       []string{"R:8081:db.internal:5432"},
			destHosts:     []string{"db.internal"},
			latencyMs:     nil,
			obsErr:        func() *string { s := "connection refused"; return &s }(),
			wantEventType: "tunnel_error",
			wantStatus:    "error",
			wantLatency:   false,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange: redirect stdout to a pipe
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe() error: %v", err)
			}
			origStdout := os.Stdout
			os.Stdout = w

			// Act
			emitObsEvent(testCorrelationID, tt.eventType, tt.status, tt.server, tt.remotes, tt.destHosts, tt.latencyMs, tt.obsErr)

			// Restore stdout and read output
			w.Close()
			os.Stdout = origStdout
			buf := make([]byte, 4096)
			n, _ := r.Read(buf)
			r.Close()
			output := strings.TrimSpace(string(buf[:n]))

			// Assert: valid JSON
			var ev jsonEvent
			if jsonErr := json.Unmarshal([]byte(output), &ev); jsonErr != nil {
				t.Fatalf("output is not valid JSON: %v\noutput: %s", jsonErr, output)
			}

			if ev.Sourcetype != "outsystemscc:tunnel" {
				t.Errorf("source_type = %q, want %q", ev.Sourcetype, "outsystemscc:tunnel")
			}
			if ev.Source != "outsystemscc" {
				t.Errorf("source = %q, want %q", ev.Source, "outsystemscc")
			}
			if ev.Host == "" {
				t.Errorf("host is empty")
			}
			if ev.CorrelationID != testCorrelationID {
				t.Errorf("correlation_id = %q, want %q", ev.CorrelationID, testCorrelationID)
			}
			if ev.Event.EventType != tt.wantEventType {
				t.Errorf("event_type = %q, want %q", ev.Event.EventType, tt.wantEventType)
			}
			if ev.Event.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", ev.Event.Status, tt.wantStatus)
			}
			if tt.wantLatency && ev.Event.LatencyMs == nil {
				t.Errorf("latency_ms is nil, want non-nil")
			}
			if !tt.wantLatency && ev.Event.LatencyMs != nil {
				t.Errorf("latency_ms = %v, want nil", *ev.Event.LatencyMs)
			}
			if tt.wantErr && ev.Event.Error == nil {
				t.Errorf("error is nil, want non-nil")
			}
			if !tt.wantErr && ev.Event.Error != nil {
				t.Errorf("error = %q, want nil", *ev.Event.Error)
			}
		})
	}
}

func Test_extractDestinationHosts(t *testing.T) {
	tests := []struct {
		name    string
		remotes []string
		want    []string
	}{
		{
			name:    "single valid remote",
			remotes: []string{"R:8081:db.internal:5432"},
			want:    []string{"db.internal"},
		},
		{
			name:    "multiple valid remotes",
			remotes: []string{"R:8081:db.internal:5432", "R:8082:cache.internal:6379"},
			want:    []string{"db.internal", "cache.internal"},
		},
		{
			name:    "invalid remote is skipped",
			remotes: []string{"not-a-valid-remote"},
			want:    []string{},
		},
		{
			name:    "mix of valid and invalid remotes",
			remotes: []string{"R:8081:db.internal:5432", "not-valid"},
			want:    []string{"db.internal"},
		},
		{
			name:    "empty remotes",
			remotes: []string{},
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := extractDestinationHosts(tt.remotes)

			// Assert
			if len(got) != len(tt.want) {
				t.Errorf("extractDestinationHosts() len = %d, want %d; got %v", len(got), len(tt.want), got)
				return
			}
			for i, h := range got {
				if h != tt.want[i] {
					t.Errorf("extractDestinationHosts()[%d] = %q, want %q", i, h, tt.want[i])
				}
			}
		})
	}
}

func Test_validateRemotes(t *testing.T) {

	tests := []struct {
		name    string
		remotes []string
		want    string
		wantErr bool
	}{
		{
			name:    "success",
			remotes: []string{"R:15800:localhost:7000", "R:15801:localhost:7001"},
			want:    "15800,15801",
			wantErr: false,
		},
		{
			name:    "error",
			remotes: []string{"R:15800:localhost:7000", "R:15800:localhost:7001"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateRemotes(tt.remotes)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRemotes() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("validateRemotes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getURL(t *testing.T) {
	tests := []struct {
		name               string
		requestLocation    string
		responseStatusCode int
		redirectLocation   string
		want               string
	}{
		{
			name:               "valid location",
			requestLocation:    "https://service.us-east-1.example.com",
			responseStatusCode: http.StatusOK,
			redirectLocation:   "",
			want:               "https://service.us-east-1.example.com",
		},

		{
			name:               "valid location - no scheme",
			requestLocation:    "service.us-east-1.example.com",
			responseStatusCode: http.StatusOK,
			redirectLocation:   "",
			want:               "http://service.us-east-1.example.com",
		},
		{
			name:               "302 redirect",
			requestLocation:    "https://service.us-east-1.example.com",
			responseStatusCode: http.StatusFound,
			redirectLocation:   "https://redirected.example.com",
			want:               "https://redirected.example.com",
		},
		{
			name:               "302 redirect - no scheme",
			requestLocation:    "service.us-east-1.example.com",
			responseStatusCode: http.StatusFound,
			redirectLocation:   "https://redirected.example.com",
			want:               "https://redirected.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := resty.New()

			// Activate httpmock for this client
			httpmock.ActivateNonDefault(client.GetClient())
			defer httpmock.DeactivateAndReset()
			mockRequestLocation := tt.requestLocation

			if !strings.HasPrefix(tt.requestLocation, "http") {
				mockRequestLocation = "http://" + mockRequestLocation
			}

			// Register a mocked response
			httpmock.RegisterResponder("GET", mockRequestLocation,
				func(req *http.Request) (*http.Response, error) {
					resp := httpmock.NewStringResponse(tt.responseStatusCode, "")
					resp.Header.Set("Location", tt.redirectLocation)
					return resp, nil
				},
			)

			got := fetchURL(client, tt.requestLocation)
			if got != tt.want {
				t.Errorf("getURL() = %v, want %v", got, tt.want)
			}
			if tt.responseStatusCode == http.StatusFound {
				// Check if the redirect location was set correctly
				httpmock.RegisterResponder("GET", mockRequestLocation,
					httpmock.NewStringResponder(tt.responseStatusCode, "Location: "+tt.redirectLocation),
				)
			}
		})
	}
}
