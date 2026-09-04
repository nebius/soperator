package reconciler

import (
	"testing"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/v25/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestMariaDbReconciler_patch(t *testing.T) {
	existing := &mariadbv1alpha1.MariaDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-acct-db",
			Namespace: "test-namespace",
		},
		Spec: mariadbv1alpha1.MariaDBSpec{
			Image: "mariadb:old",
			MyCnf: ptr.To("[mariadb]\ninnodb_buffer_pool_size=32768M"),
		},
	}
	desired := &mariadbv1alpha1.MariaDB{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-acct-db",
			Namespace: "test-namespace",
		},
		Spec: mariadbv1alpha1.MariaDBSpec{
			Image: "mariadb:new",
			MyCnf: ptr.To("[mariadb]\ninnodb_buffer_pool_size=2048M"),
		},
	}

	patch, err := (&MariaDbReconciler{}).patch(existing, desired)

	assert.NoError(t, err)
	assert.NotNil(t, patch)
	assert.Equal(t, desired.Spec.Image, existing.Spec.Image)
	assert.Equal(t, desired.Spec.MyCnf, existing.Spec.MyCnf)
}
