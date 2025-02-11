package jwt_test

import (
	"context"
	"testing"
	"time"

	jwtImpl "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/pkg/jwt"
)

const (
	clusterNamespace = "default"
	clusterName      = "slurm"
	username         = "user"
)

var (
	secretName    = naming.BuildSecretSlurmRESTSecretName(clusterName)
	signingKey, _ = jwt.GenerateSigningKey()
	cluster       = types.NamespacedName{
		Namespace: clusterNamespace,
		Name:      clusterName,
	}
)

func TestToken_Validation(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	token := jwt.NewToken(client)

	// Missing cluster and username
	_, err := token.Issue(context.TODO())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "meta is not provided")

	// Missing namespace
	token.For(types.NamespacedName{Name: clusterName}, username)
	_, err = token.Issue(context.TODO())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cluster namespace is not provided")

	// Missing cluster name
	token.For(types.NamespacedName{Namespace: clusterNamespace}, username)
	_, err = token.Issue(context.TODO())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cluster name is not provided")

	// Missing username
	token.For(types.NamespacedName{Namespace: clusterNamespace, Name: clusterName}, "")
	_, err = token.Issue(context.TODO())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "username is not provided")
}

func TestToken_Issue_Success(t *testing.T) {
	client := fake.NewClientBuilder().WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			consts.SecretRESTJWTKeyFileName: signingKey,
		},
	}).Build()

	token, err := jwt.NewToken(client).
		For(cluster, username).
		Issue(context.TODO())

	assert.NoError(t, err, "Token issuance should not fail")
	assert.NotEmpty(t, token, "Signed token should not be empty")

	parsedToken, err := jwtImpl.Parse(
		token,
		func(t *jwtImpl.Token) (interface{}, error) {
			return signingKey, nil
		},
		jwtImpl.WithValidMethods([]string{jwtImpl.SigningMethodHS256.Alg()}),
	)

	assert.NoError(t, err, "Parsing the signed token should not fail")
	assert.True(t, parsedToken.Valid, "The token should be valid")
	claims, ok := parsedToken.Claims.(jwtImpl.MapClaims)
	assert.True(t, ok, "Claims should be of type jwt.MapClaims")
	assert.Equalf(t, username, claims["sub"], "Token subject should be %q", username)
}

func TestToken_Issue_WithRegistry(t *testing.T) {
	client := fake.NewClientBuilder().WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			consts.SecretRESTJWTKeyFileName: signingKey,
		},
	}).Build()
	registry := jwt.NewTokenRegistry().Build()
	token, err := jwt.NewToken(client).
		For(cluster, username).
		WithRegistry(registry).
		Issue(context.TODO())

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Retrieve from registry
	cachedToken, err := jwt.NewToken(client).
		For(cluster, username).
		WithRegistry(registry).
		Issue(context.TODO())

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	assert.Equal(t, token, cachedToken, "Cached token should match the previously issued token")
}

func TestToken_Issue_SecretNotFound(t *testing.T) {
	client := fake.NewClientBuilder().Build()

	_, err := jwt.NewToken(client).
		For(cluster, username).
		Issue(context.TODO())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get signing secret", "Should return an error when the secret is not found")
}

func TestToken_WithLifetime(t *testing.T) {
	client := fake.NewClientBuilder().WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: clusterNamespace,
			Name:      secretName,
		},
		Data: map[string][]byte{
			consts.SecretRESTJWTKeyFileName: signingKey,
		},
	}).Build()
	registry := jwt.NewTokenRegistry().Build()
	token, err := jwt.NewToken(client).
		For(cluster, username).
		WithRegistry(registry).
		WithLifetime(time.Second).
		Issue(context.TODO())

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	// Wait for the token to expire
	time.Sleep(time.Second * 2)

	// Retrieve from registry
	newToken, err := jwt.NewToken(client).
		For(cluster, username).
		WithRegistry(registry).
		Issue(context.TODO())

	assert.NoError(t, err)
	assert.NotEmpty(t, token)

	assert.NotEqual(t, token, newToken, "New token should not match expired token")
}
