package jwt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

// TokenMeta represents inner metadata about a token to define its subject.
type TokenMeta struct {
	cluster  types.NamespacedName
	username string

	annotations map[string]string
}

// String implements fmt.Stringer interface.
// Use it to get the key for the token.
func (m TokenMeta) String() string {
	res := fmt.Sprintf("%s/%s:%s", m.cluster, m.username, m.username)

	if m.annotations == nil {
		return res
	}

	as := make([]string, len(m.annotations))
	for k, v := range m.annotations {
		as = append(
			as,
			fmt.Sprintf("%s=%s", k, v),
		)
	}
	res = fmt.Sprintf("%s:{%s}", res, strings.Join(as, ","))

	return res
}

// TokenClaims extends the standard JWT claims with additional support for a Slurm username.
type TokenClaims struct {
	jwt.RegisteredClaims

	SlurmUsername string `json:"sun,omitempty"`
}

// GetSlurmUsername returns the Slurm username from the token claims.
func (c TokenClaims) GetSlurmUsername() (string, error) {
	return c.SlurmUsername, nil
}

// Token is a builder for issuing JWT tokens for Slurm clusters.
type Token struct {
	client client.Client

	meta     *TokenMeta
	lifetime time.Duration

	registry *TokenRegistry
}

// NewToken creates a new Token with DefaultTokenLifetime.
func NewToken(client client.Client) *Token {
	return &Token{
		client:   client,
		lifetime: DefaultTokenLifetime,
	}
}

// For specifies the Slurm cluster and username for which the token will be issued.
//
// Required.
func (t *Token) For(cluster types.NamespacedName, username string) *Token {
	if t.meta == nil {
		t.meta = &TokenMeta{}
	}

	t.meta.cluster = cluster
	t.meta.username = username

	return t
}

// WithAnnotations adds annotations to distinguish tokens for the same user.
func (t *Token) WithAnnotations(annotations map[string]string) *Token {
	if t.meta == nil {
		t.meta = &TokenMeta{}
	}

	t.meta.annotations = make(map[string]string, len(annotations))
	for k, v := range annotations {
		t.meta.annotations[k] = v
	}

	return t
}

// WithLifetime sets non-(DefaultTokenLifetime) for the token.
func (t *Token) WithLifetime(lifetime time.Duration) *Token {
	t.lifetime = lifetime
	return t
}

// WithRegistry associates a TokenRegistry with the token for caching.
func (t *Token) WithRegistry(registry *TokenRegistry) *Token {
	t.registry = registry
	return t
}

// validate checks the token configuration before issuing.
func (t *Token) validate() error {
	if t.client == nil {
		return errors.New("k8s client is not provided")
	}

	if t.meta == nil {
		return errors.New("meta is not provided. it's unclear whom to issue token to")
	}
	if t.meta.cluster.Namespace == "" {
		return errors.New("cluster namespace is not provided")
	}
	if t.meta.cluster.Name == "" {
		return errors.New("cluster name is not provided")
	}
	if t.meta.username == "" {
		return errors.New("username is not provided")
	}

	return nil
}

// Issue issues a signed JWT token with specified parameters.
func (t *Token) Issue(ctx context.Context) (string, error) {
	if err := t.validate(); err != nil {
		return "", errors.Wrap(err, "failed to issue token")
	}

	if t.registry != nil {
		token, found := t.registry.Get(t.meta.String())
		if found {
			return token, nil
		}
	}

	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:  "soperator",
			Subject: t.meta.username,
			ID:      string(uuid.NewUUID()),
		},
		SlurmUsername: t.meta.username,
	}
	{
		issuedAt := time.Now()
		claims.NotBefore = jwt.NewNumericDate(issuedAt)
		claims.IssuedAt = jwt.NewNumericDate(issuedAt)
		claims.ExpiresAt = jwt.NewNumericDate(issuedAt.Add(t.lifetime))
	}

	token := jwt.NewWithClaims(
		jwt.SigningMethodHS256,
		claims,
	)

	signingKey, err := t.getSigningKey(ctx)
	if err != nil {
		return "", errors.Wrap(err, "failed to get signing key")
	}

	signedToken, err := token.SignedString(signingKey)
	if err != nil {
		return "", errors.Wrap(err, "failed to sign token")
	}

	if t.registry != nil {
		t.registry.RegisterWithLifetime(t.meta.String(), signedToken, t.lifetime)
	}

	return signedToken, nil
}

// getSigningKey retrieves the signing key from a Kubernetes secret.
func (t *Token) getSigningKey(ctx context.Context) ([]byte, error) {
	signingKeySecret := corev1.Secret{}
	if err := t.client.Get(
		ctx,
		types.NamespacedName{
			Namespace: t.meta.cluster.Namespace,
			Name:      naming.BuildSecretSlurmRESTSecretName(t.meta.cluster.Name),
		},
		&signingKeySecret,
	); err != nil {
		return nil, errors.Wrap(err, "failed to get signing secret")
	}

	return signingKeySecret.Data[consts.SecretRESTJWTKeyFileName], nil
}
