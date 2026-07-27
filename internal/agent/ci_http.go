package agent

import (
	"net/http"
	"time"

	"github.com/charliewilco/transwarp/internal/tunnelnet"
)

var newCIHTTPClient = func(timeout time.Duration) *http.Client {
	client := tunnelnet.NoRedirectHTTPClient()
	client.Timeout = timeout
	return client
}
