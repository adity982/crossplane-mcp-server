package crossplane

import (
	"fmt"
	"sync"

	"github.com/ravibagri5/crossplane-mcp-server/pkg/kube"
)

// Provider hands out a Client per cluster.
//
// Clients are built lazily and then cached, because constructing one performs
// discovery against the API server. A server watching a fleet would otherwise
// pay that cost on every tool call.
type Provider struct {
	loader *kube.Loader
	// staticDefault names the single target when there is no loader.
	staticDefault string

	mu      sync.Mutex
	clients map[string]*Client
}

// NewProvider returns a Provider over the clusters the loader can see.
func NewProvider(loader *kube.Loader) *Provider {
	return &Provider{loader: loader, clients: map[string]*Client{}}
}

// NewStaticProvider returns a Provider serving one already built client,
// for callers that manage credentials themselves and for tests.
func NewStaticProvider(name string, client *Client) *Provider {
	client.target = name
	return &Provider{
		staticDefault: name,
		clients:       map[string]*Client{name: client},
	}
}

// Targets lists the clusters this server can talk to.
func (p *Provider) Targets() []kube.Target {
	if p.loader == nil {
		return []kube.Target{{Name: p.staticDefault, Source: kube.SourceInCluster, Default: true}}
	}
	return p.loader.Targets()
}

// Default returns the cluster used when a caller does not name one.
func (p *Provider) Default() string {
	if p.loader == nil {
		return p.staticDefault
	}
	return p.loader.Default()
}

// Client returns a client for one cluster. An empty name selects the default.
//
// Connection errors are reported here rather than at startup, so that one
// unreachable cluster in a fleet does not stop the server from serving the
// others.
func (p *Provider) Client(name string) (*Client, error) {
	if name == "" {
		name = p.Default()
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if client, ok := p.clients[name]; ok {
		return client, nil
	}
	if p.loader == nil {
		return nil, fmt.Errorf("unknown cluster %q", name)
	}

	resolved, err := p.loader.Resolve(name)
	if err != nil {
		return nil, err
	}
	client, err := New(resolved.RESTConfig, resolved.Namespace)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to cluster %q: %w", name, err)
	}
	client.target = resolved.Target

	p.clients[name] = client
	return client, nil
}
