package slurmapi

import (
	"maps"
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

type ClientSet struct {
	slurmAPIClients map[types.NamespacedName]Client
	mux             *sync.Mutex
}

func NewClientSet() *ClientSet {
	return &ClientSet{
		slurmAPIClients: make(map[types.NamespacedName]Client),
		mux:             &sync.Mutex{},
	}
}

func (cs *ClientSet) AddClient(name types.NamespacedName, client Client) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	cs.slurmAPIClients[name] = client
}

func (cs *ClientSet) GetClient(name types.NamespacedName) (Client, bool) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	client, found := cs.slurmAPIClients[name]
	return client, found
}

func (cs *ClientSet) GetClients() (slurmAPIClients map[types.NamespacedName]Client) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	return maps.Clone(cs.slurmAPIClients)
}
