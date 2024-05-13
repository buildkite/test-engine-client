package api

type client struct {
	ServerBaseUrl string
	AccessToken   string
}

type ClientConfig struct {
	ServerBaseUrl string
	AccessToken   string
}

func NewClient(cfg ClientConfig) *client {
	return &client{
		ServerBaseUrl: cfg.ServerBaseUrl,
	}
}
