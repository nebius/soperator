package slurmproxy

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

type ClientSet struct {
	clients map[types.NamespacedName]Interface
	mux     *sync.Mutex
}

func NewClientSet() *ClientSet {
	return &ClientSet{
		clients: make(map[types.NamespacedName]Interface),
		mux:     &sync.Mutex{},
	}
}

func (cs *ClientSet) AddClient(name types.NamespacedName, client Interface) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	cs.clients[name] = client
}

func (cs *ClientSet) GetClient(name types.NamespacedName) (Interface, bool) {
	cs.mux.Lock()
	defer cs.mux.Unlock()

	client, found := cs.clients[name]
	return client, found
}
