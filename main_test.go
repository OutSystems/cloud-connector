package main

import (
	"net/http"
	"testing"
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

// mockRoundTripper is a custom implementation of http.RoundTripper for mocking HTTP responses.
type mockRoundTripper struct {
	mockResponse func(req *http.Request) *http.Response
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.mockResponse(req), nil
}

func Test_getURL(t *testing.T) {
	tests := []struct {
		name            string
		requestLocation string
		mockResponse    func(req *http.Request) *http.Response
		want            string
	}{
		{
			name:            "valid location",
			requestLocation: "https://service.us-east-1.example.com",
			mockResponse: func(req *http.Request) *http.Response {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       http.NoBody,
					Header:     make(http.Header),
				}
			},
			want: "https://service.us-east-1.example.com",
		},
		{
			name:            "302 redirect",
			requestLocation: "https://service.us-east-1.example.com",
			mockResponse: func(req *http.Request) *http.Response {
				return &http.Response{
					StatusCode: http.StatusFound,
					Body:       http.NoBody,
					Header: http.Header{
						"Location": []string{"https://redirected.example.com"},
					},
				}
			},
			want: "https://redirected.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a custom HTTP client with the mock RoundTripper
			client := &http.Client{
				Transport: &mockRoundTripper{
					mockResponse: tt.mockResponse,
				},
			}

			got := getURL(client, tt.requestLocation)
			if got != tt.want {
				t.Errorf("getURL() = %v, want %v", got, tt.want)
			}
		})
	}
}
