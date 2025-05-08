package main

import (
	"net/http"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/jarcoal/httpmock"
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

type MockClient struct {
	mockResponse *resty.Response
	mockError    error
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
			name:               "302 redirect",
			requestLocation:    "https://service.us-east-1.example.com",
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

			// Register a mocked response
			if tt.responseStatusCode == http.StatusFound {
				httpmock.RegisterResponder("GET", tt.requestLocation,
					func(req *http.Request) (*http.Response, error) {
						resp := httpmock.NewStringResponse(tt.responseStatusCode, "")
						resp.Header.Set("Location", tt.redirectLocation)
						return resp, nil
					},
				)
			} else {
				httpmock.RegisterResponder("GET", tt.requestLocation,
					httpmock.NewStringResponder(tt.responseStatusCode, ""),
				)
			}

			got := getURL(client, tt.requestLocation)
			if got != tt.want {
				t.Errorf("getURL() = %v, want %v", got, tt.want)
			}
			if tt.responseStatusCode == http.StatusFound {
				// Check if the redirect location was set correctly
				httpmock.RegisterResponder("GET", tt.requestLocation,
					httpmock.NewStringResponder(tt.responseStatusCode, "Location: "+tt.redirectLocation),
				)
			}
		})
	}
}
