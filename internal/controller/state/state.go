package state

import (
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type reconciliationState struct {
	data map[string]any
}

var (
	ReconciliationState = reconciliationState{
		data: map[string]any{},
	}
)

func (s *reconciliationState) ensureData() {
	if s.data == nil {
		s.data = map[string]any{}
	}
}

func (s *reconciliationState) buildKey(kind schema.ObjectKind, key client.ObjectKey) string {
	return strings.Join([]string{kind.GroupVersionKind().String(), key.String()}, "/")
}

func (s *reconciliationState) Set(kind schema.ObjectKind, key client.ObjectKey) {
	s.ensureData()

	s.data[s.buildKey(kind, key)] = nil
}

func (s *reconciliationState) Present(kind schema.ObjectKind, key client.ObjectKey) bool {
	s.ensureData()

	_, found := s.data[s.buildKey(kind, key)]
	return found
}

func (s *reconciliationState) Remove(kind schema.ObjectKind, key client.ObjectKey) {
	delete(s.data, s.buildKey(kind, key))
}
