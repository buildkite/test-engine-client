package api

import (
	"net/http"
)

type client struct {
	OrganizationSlug string
	ServerBaseUrl    string
	AccessToken      string
	httpClient       *http.Client
}

type ClientConfig struct {
	AccessToken      string
	OrganizationSlug string
	ServerBaseUrl    string
}

type authTransport struct {
	accessToken string
}

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.accessToken)
	return http.DefaultTransport.RoundTrip(req)
}

func NewClient(cfg ClientConfig) *client {
	httpClient := &http.Client{
		Transport: &authTransport{
			accessToken: cfg.AccessToken,
		},
	}

	return &client{
		OrganizationSlug: cfg.OrganizationSlug,
		AccessToken:      cfg.AccessToken,
		ServerBaseUrl:    cfg.ServerBaseUrl,
		httpClient:       httpClient,
	}
}
