package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/render/common"
)

func Test_RenderSecret(t *testing.T) {

	secret, err := accounting.RenderSecret(defaultNamespace, defaultNameCluster, acc, defaultSecret, false)
	assert.NoError(t, err)
	assert.NotNil(t, secret)
	assert.Equal(t, naming.BuildSecretSlurmdbdConfigsName(defaultNameCluster), secret.Name)
	assert.Equal(t, defaultNamespace, secret.Namespace)
	assert.Equal(t, common.RenderLabels(consts.ComponentTypeAccounting, defaultNameCluster), secret.Labels)

	_, ok := secret.Data[consts.ConfigMapKeySlurmdbdConfig]
	assert.True(t, ok)
}

func Test_RenderSecret_Errors(t *testing.T) {
	testAcc := *acc
	// Test with nil secret
	_, err := accounting.RenderSecret(defaultNamespace, defaultNameCluster, nil, nil, false)
	assert.Equal(t, accounting.ErrSecretNil, err.Error())

	// // Test with empty secret data
	testSecret := &corev1.Secret{}
	_, err = accounting.RenderSecret(defaultNamespace, defaultNameCluster, acc, testSecret, false)
	assert.Equal(t, accounting.ErrSecretDataEmpty, err.Error())

	// // Test with empty external DB user
	testAcc.ExternalDB.User = ""
	_, err = accounting.RenderSecret(defaultNamespace, defaultNameCluster, &testAcc, defaultSecret, false)
	assert.Equal(t, accounting.ErrDBUserEmpty, err.Error())

	// // Test with empty external DB host
	testAcc = *acc
	testAcc.ExternalDB.Host = ""
	_, err = accounting.RenderSecret(defaultNamespace, defaultNameCluster, &testAcc, defaultSecret, false)
	assert.Equal(t, accounting.ErrDBHostEmpty, err.Error())

	// // Test with missing password key
	testAcc = *acc
	testAcc.ExternalDB.PasswordSecretKeyRef.Key = "missing-key"
	_, err = accounting.RenderSecret(defaultNamespace, defaultNameCluster, &testAcc, defaultSecret, false)
	assert.Equal(t, accounting.ErrPasswordKeyMissing, err.Error())

	// // Test with empty password
	testSecret = defaultSecret.DeepCopy()
	testSecret.Data = map[string][]byte{
		passwordKey: []byte(""),
	}
	_, err = accounting.RenderSecret(defaultNamespace, defaultNameCluster, acc, testSecret, false)
	assert.Equal(t, accounting.ErrPasswordEmpty, err.Error())
}
