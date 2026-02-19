package provider

import (
	"fmt"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Type()] = p
}

func (r *Registry) Get(channelType string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[channelType]
	if !ok {
		return nil, fmt.Errorf("no provider registered for channel type: %s", channelType)
	}
	return p, nil
}
