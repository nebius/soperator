package controller_test

import (
	"context"
	"time"

	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func eventuallyGetNamespacedObj(ctx context.Context, client client.Client, namespace, name string, obj client.Object) {
	Eventually(func() bool {
		err := client.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, obj)
		return err == nil
	}, time.Second*10, time.Millisecond*300).Should(BeTrue())
}
