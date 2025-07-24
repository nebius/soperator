package accounting

import (
	"errors"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

const (
	ErrAccountingNil      = "accounting is nil"
	ErrSecretNil          = "secret is nil"
	ErrSecretDataEmpty    = "secret data is empty"
	ErrDBUserEmpty        = "external DB user is empty"
	ErrDBHostEmpty        = "external DB host is empty"
	ErrPasswordKeyMissing = "secret data does not contain password key"
	ErrPasswordEmpty      = "password is empty"

	StorageParameterSSLClientCert = "SSL_CERT"
	StorageParameterSSLClientKey  = "SSL_KEY"
	StorageParameterSSLCACert     = "SSL_CA"
)

func RenderSecret(
	namespace,
	clusterName string,
	accounting *values.SlurmAccounting,
	passwordSecret *corev1.Secret,
	isRESTenabled bool,
) (*corev1.Secret, error) {
	var err error
	passwordName := make([]byte, 0)
	if accounting == nil {
		return nil, errors.New("accounting is nil")
	}

	if passwordSecret != nil {
		passwordName, err = checkPasswordSecret(accounting, passwordSecret)
		if err != nil {
			return nil, err
		}
	}
	secretName := naming.BuildSecretSlurmdbdConfigsName(clusterName)
	labels := common.RenderLabels(consts.ComponentTypeAccounting, clusterName)
	labels[consts.LabelSConfigControllerSourceKey] = consts.LabelSConfigControllerSourceValue
	data := map[string][]byte{
		consts.ConfigMapKeySlurmdbdConfig: []byte(generateSlurdbdConfig(
			clusterName, accounting, passwordName, isRESTenabled).Render(),
		),
	}

	annotains := map[string]string{
		consts.AnnotationSConfigControllerSourceKey: consts.DefaultSConfigControllerSourcePath,
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotains,
		},
		Data: data,
	}, nil
}

func checkPasswordSecret(accounting *values.SlurmAccounting, secret *corev1.Secret) ([]byte, error) {
	if secret == nil {
		return nil, errors.New(ErrSecretNil)
	}

	if len(secret.Data) == 0 {
		return nil, errors.New(ErrSecretDataEmpty)
	}

	var passwordName []byte
	var exists bool

	if accounting.ExternalDB.Enabled {
		if accounting.ExternalDB.User == "" {
			return nil, errors.New(ErrDBUserEmpty)
		}

		if accounting.ExternalDB.Host == "" {
			return nil, errors.New(ErrDBHostEmpty)
		}

		passwordName, exists = secret.Data[accounting.ExternalDB.PasswordSecretKeyRef.Key]
		if !exists {
			return nil, errors.New(ErrPasswordKeyMissing)
		}
	} else if accounting.MariaDb.Enabled {
		passwordName, exists = secret.Data[consts.MariaDbPasswordKey]
		if !exists {
			return nil, errors.New(ErrPasswordKeyMissing)
		}
	}

	if len(passwordName) == 0 {
		return nil, errors.New(ErrPasswordEmpty)
	}

	return passwordName, nil
}

func generateSlurdbdConfig(
	clusterName string,
	accounting *values.SlurmAccounting,
	passwordName []byte,
	isRESTenabled bool,
) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	// Unmodifiable parameters
	res.AddProperty("AuthType", "auth/"+consts.Munge)
	// TODO: Add debug level to CRD and make it configurable
	res.AddProperty("SlurmUser", consts.SlurmUser)
	res.AddProperty("LogFile", consts.SlurmLogFile)
	res.AddProperty("PidFile", consts.SlurmdbdPidFile)
	res.AddProperty("DbdHost", consts.HostnameAccounting)
	res.AddProperty("DbdPort", consts.DefaultAccountingPort)
	res.AddProperty("StorageLoc", "slurm_acct_db")
	res.AddProperty("StorageType", "accounting_storage/mysql")
	if len(passwordName) > 0 {
		res.AddProperty("StoragePass", string(passwordName))
	}
	if accounting.MariaDb.Enabled {
		res.AddProperty("StorageUser", consts.MariaDbUsername)
		res.AddProperty("StorageHost", naming.BuildMariaDbName(clusterName))
		res.AddProperty("StoragePort", accounting.MariaDb.Port)
	} else {
		res.AddProperty("StorageUser", accounting.ExternalDB.User)
		res.AddProperty("StorageHost", accounting.ExternalDB.Host)
		res.AddProperty("StoragePort", accounting.ExternalDB.Port)
		storageParameters := generateSlurmdbdConfigStorageParameters(accounting)
		if storageParameters != "" {
			res.AddProperty("StorageParameters", storageParameters)
		}
	}
	if isRESTenabled {
		res.AddComment("")
		res.AddComment("REST API settings")
		res.AddProperty("AuthAltTypes", "auth/jwt")
		res.AddProperty("AuthAltParameters", "jwt_key="+consts.SlurmdbdRESTJWTKeyPath)
	}

	// Modifiable parameters
	res.AddProperty("ArchiveEvents", accounting.SlurmdbdConfig.ArchiveEvents)
	res.AddProperty("ArchiveJobs", accounting.SlurmdbdConfig.ArchiveJobs)
	res.AddProperty("ArchiveResvs", accounting.SlurmdbdConfig.ArchiveResvs)
	res.AddProperty("ArchiveSteps", accounting.SlurmdbdConfig.ArchiveSteps)
	res.AddProperty("ArchiveSuspend", accounting.SlurmdbdConfig.ArchiveSuspend)
	res.AddProperty("ArchiveTXN", accounting.SlurmdbdConfig.ArchiveTXN)
	res.AddProperty("ArchiveUsage", accounting.SlurmdbdConfig.ArchiveUsage)
	res.AddProperty("DebugLevel", accounting.SlurmdbdConfig.DebugLevel)
	res.AddProperty("TCPTimeout", accounting.SlurmdbdConfig.TCPTimeout)
	res.AddProperty("PurgeEventAfter", accounting.SlurmdbdConfig.PurgeEventAfter)
	res.AddProperty("PurgeJobAfter", accounting.SlurmdbdConfig.PurgeJobAfter)
	res.AddProperty("PurgeResvAfter", accounting.SlurmdbdConfig.PurgeResvAfter)
	res.AddProperty("PurgeStepAfter", accounting.SlurmdbdConfig.PurgeStepAfter)
	res.AddProperty("PurgeSuspendAfter", accounting.SlurmdbdConfig.PurgeSuspendAfter)
	res.AddProperty("PurgeTXNAfter", accounting.SlurmdbdConfig.PurgeTXNAfter)
	res.AddProperty("PurgeUsageAfter", accounting.SlurmdbdConfig.PurgeUsageAfter)
	if accounting.SlurmdbdConfig.PrivateData != "" {
		res.AddProperty("PrivateData", accounting.SlurmdbdConfig.PrivateData)
	}
	if accounting.SlurmdbdConfig.DebugFlags != "" {
		res.AddProperty("DebugFlags", accounting.SlurmdbdConfig.DebugFlags)
	}

	return res
}

func generateSlurmdbdConfigStorageParameters(accounting *values.SlurmAccounting) string {
	spValues := map[string]string{}
	for k, v := range accounting.ExternalDB.StorageParameters {
		spValues[k] = v
	}

	if accounting.ExternalDB.TLS.ServerCASecretRef != "" {
		spValues[StorageParameterSSLCACert] = consts.VolumeMountPathSlurmdbdSSLCACertificate + "/" +
			consts.SecretSlurmdbdSSLServerCACertificateFile
	}

	if accounting.ExternalDB.TLS.ClientCertSecretRef != "" {
		spValues[StorageParameterSSLClientCert] = consts.VolumeMountPathSlurmdbdSSLClientKey + "/" +
			consts.SecretSlurmdbdSSLClientKeyCertificateFile
		spValues[StorageParameterSSLClientKey] = consts.VolumeMountPathSlurmdbdSSLClientKey + "/" +
			consts.SecretSlurmdbdSSLClientKeyPrivateKeyFile
	}

	valuesList := make([]string, 0, len(spValues))
	for k, v := range spValues {
		valuesList = append(valuesList, k+"="+v)
	}
	sort.Strings(valuesList)
	return strings.Join(valuesList, ",")
}
