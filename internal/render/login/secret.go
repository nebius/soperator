package login

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

// region Sshd keys
func generateECDSAKeyPair() (map[string][]byte, error) {
	privKey, err := ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	if err != nil {
		return nil, err
	}

	privDER, err := x509.MarshalECPrivateKey(privKey)
	if err != nil {
		return nil, err
	}

	privBlock := pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privDER,
	}

	privKeyBytes := pem.EncodeToMemory(&privBlock)

	pubKey, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, err
	}

	pubKeyBytes := ssh.MarshalAuthorizedKey(pubKey)

	return map[string][]byte{
		consts.SecretSshdECDSAKeyName:    privKeyBytes,
		consts.SecretSshdECDSAPubKeyName: pubKeyBytes,
	}, nil
}

func generateED25519KeyPair() (map[string][]byte, error) {
	pubKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	privateKeyMarshaled, err := ssh.MarshalPrivateKey(crypto.PrivateKey(privateKey), "")
	if err != nil {
		return nil, err
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyMarshaled)

	pubKeySSH, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, err
	}
	pubKeyBytes := ssh.MarshalAuthorizedKey(pubKeySSH)

	return map[string][]byte{
		consts.SecretSshdECDSA25519KeyName:    privateKeyBytes,
		consts.SecretSshdECDSA25519PubKeyName: pubKeyBytes,
	}, nil
}

func generateRSAKeyPair() (map[string][]byte, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	privDER := x509.MarshalPKCS1PrivateKey(privKey)

	privBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}

	privKeyBytes := pem.EncodeToMemory(&privBlock)

	pubKey, err := ssh.NewPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, err
	}

	pubKeyBytes := ssh.MarshalAuthorizedKey(pubKey)

	return map[string][]byte{
		consts.SecretSshdRSAKeyName:    privKeyBytes,
		consts.SecretSshdRSAPubKeyName: pubKeyBytes,
	}, nil
}

func collectSshdKeyPairsData() (map[string][]byte, error) {
	ecdsaKeys, err := generateECDSAKeyPair()
	if err != nil {
		return nil, fmt.Errorf("error generating ECDSA key pair: %w", err)
	}

	ed25519Keys, err := generateED25519KeyPair()
	if err != nil {
		return nil, fmt.Errorf("error generating ED25519 key pair: %w", err)
	}

	rsaKeys, err := generateRSAKeyPair()
	if err != nil {
		return nil, fmt.Errorf("error generating RSA key pair: %w", err)
	}

	allKeys := make(map[string][]byte)
	for k, v := range ecdsaKeys {
		allKeys[k] = v
	}
	for k, v := range ed25519Keys {
		allKeys[k] = v
	}
	for k, v := range rsaKeys {
		allKeys[k] = v
	}
	return allKeys, nil
}

// RenderSSHDKeysSecret renders new [corev1.Secret] containing sshd server key pairs
func RenderSSHDKeysSecret(cluster *values.SlurmCluster) (corev1.Secret, error) {
	data, err := collectSshdKeyPairsData()
	if err != nil {
		return corev1.Secret{}, fmt.Errorf("error collecting SSHD key pairs: %w", err)
	}
	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildSecretSSHDKeysName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    common.RenderLabels(consts.ComponentTypeLogin, cluster.Name),
		},
		Data: data,
	}, nil
}

// endregion Sshd keys
