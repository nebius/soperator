package reconciler

import (
	"context"
	"net/http"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeGoneClient struct {
	client.Client
}

func (c *fakeGoneClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return &apierrors.StatusError{
		ErrStatus: metav1.Status{
			Status:  metav1.StatusFailure,
			Code:    http.StatusGone,
			Reason:  metav1.StatusReasonGone,
			Message: "the resource is gone",
		},
	}
}

type fakeErrorClient struct {
	client.Client
}

func (c *fakeErrorClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	return errors.New("delete error")
}
