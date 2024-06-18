package logfield

import (
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

func ResourceKV(obj client.Object) []any {
	var kind string
	t := reflect.TypeOf(obj)
	if t.Kind() == reflect.Ptr {
		kind = t.Elem().Name()
	} else {
		kind = t.Name()
	}
	return []any{
		ResourceKind, kind,
		ResourceName, obj.GetName(),
	}
}
