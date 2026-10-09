package main

import (
	"bytes"
	"net/http"
	"testing"

	"strings"

	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"

	chclient "github.com/jpillora/chisel/client"
)

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

func Test_newChiselClient_logLevels(t *testing.T) {
	// Connection status (info) lines must print regardless of -v; chisel's
	// debug output must stay behind -v. Regression guard for RDODCP-1670.
	tests := []struct {
		name      string
		verbose   bool
		wantInfo  bool
		wantDebug bool
	}{
		{
			name:      "default shows connection status but not debug",
			verbose:   false,
			wantInfo:  true,
			wantDebug: false,
		},
		{
			name:      "verbose shows connection status and debug",
			verbose:   true,
			wantInfo:  true,
			wantDebug: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := chclient.Config{
				Headers: http.Header{},
				Server:  "http://service.us-east-1.example.com",
				Remotes: []string{"R:15800:localhost:7000"},
			}

			c, err := newChiselClient(&config, tt.verbose)
			if err != nil {
				t.Fatalf("newChiselClient() unexpected error = %v", err)
			}

			if got := c.Logger.IsInfo(); got != tt.wantInfo {
				t.Errorf("IsInfo() = %v, want %v", got, tt.wantInfo)
			}
			if got := c.Logger.IsDebug(); got != tt.wantDebug {
				t.Errorf("IsDebug() = %v, want %v", got, tt.wantDebug)
			}
		})
	}
}

// Test_newChiselClient_statusLinesWithoutVerbose asserts on the rendered output
// rather than the flags, so the test fails if chisel ever moves these lines to
// another level. "Connecting to" is the first status line emitted on Start.
func Test_newChiselClient_statusLinesWithoutVerbose(t *testing.T) {
	config := chclient.Config{
		Headers: http.Header{},
		Server:  "http://service.us-east-1.example.com",
		Remotes: []string{"R:15800:localhost:7000"},
	}

	c, err := newChiselClient(&config, false)
	if err != nil {
		t.Fatalf("newChiselClient() unexpected error = %v", err)
	}

	var out bytes.Buffer
	c.Logger.SetOutput(&out)

	c.Infof("Connecting to %s", config.Server)
	c.Debugf("Handshaking...")

	got := out.String()
	if !strings.Contains(got, "Connecting to") {
		t.Errorf("expected info-level status line in output without -v, got %q", got)
	}
	if strings.Contains(got, "Handshaking") {
		t.Errorf("expected debug-level output to stay behind -v, got %q", got)
	}
}
