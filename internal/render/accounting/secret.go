package accounting

import (
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/values"
)

const (
	ErrSecretNil          = "secret is nil"
	ErrSecretDataEmpty    = "secret data is empty"
	ErrDBUserEmpty        = "external DB user is empty"
	ErrDBHostEmpty        = "external DB host is empty"
	ErrPasswordKeyMissing = "secret data does not contain password key"
	ErrPasswordEmpty      = "password is empty"
)

func RenderSecret(
	namespace,
	clusterName string,
	accounting *values.SlurmAccounting,
	secret *corev1.Secret,
) (*corev1.Secret, error) {
	passwordName, err := checkSecret(accounting, secret)
	if err != nil {
		return nil, err
	}
	secretName := naming.BuildSecretSlurmdbdConfigsName(clusterName)
	labels := common.RenderLabels(consts.ComponentTypeAccounting, clusterName)
	data := map[string][]byte{
		consts.ConfigMapKeySlurmdbdConfig: []byte(generateSlurdbdConfig(clusterName, accounting, passwordName).Render()),
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: data,
	}, nil
}

func checkSecret(accounting *values.SlurmAccounting, secret *corev1.Secret) ([]byte, error) {
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

func generateSlurdbdConfig(clusterName string, accounting *values.SlurmAccounting, passwordName []byte) renderutils.ConfigFile {
	res := &renderutils.PropertiesConfig{}
	// TODO: Add support switch ExternalDB and MariaDB CRD. Now we just support ExternalDB
	// Unmodifiable parameters
	res.AddProperty("AuthType", "auth/"+consts.Munge)
	// TODO: Add debug level to CRD and make it configurable
	res.AddProperty("DebugLevel", consts.SlurmDefaultDebugLevel)
	res.AddProperty("SlurmUser", consts.SlurmUser)
	res.AddProperty("LogFile", consts.SlurmLogFile)
	res.AddProperty("PidFile", consts.SlurmdbdPidFile)
	res.AddProperty("DbdHost", consts.HostnameAccounting)
	res.AddProperty("DbdPort", consts.DefaultAccountingPort)
	res.AddProperty("StorageLoc", "slurm_acct_db")
	res.AddProperty("StorageType", "accounting_storage/mysql")
	res.AddProperty("StoragePass", string(passwordName))
	if accounting.MariaDb.Enabled {
		res.AddProperty("StorageUser", consts.MariaDbUsername)
		res.AddProperty("StorageHost", naming.BuildMariaDbName(clusterName))
		res.AddProperty("StoragePort", accounting.MariaDb.Port)
	} else {
		res.AddProperty("StorageUser", accounting.ExternalDB.User)
		res.AddProperty("StorageHost", accounting.ExternalDB.Host)
		res.AddProperty("StoragePort", accounting.ExternalDB.Port)
	}

	// TODO: make it configurable through CRD
	// Modifiable parameters
	res.AddProperty("ArchiveEvents", "yes")
	res.AddProperty("ArchiveJobs", "yes")
	res.AddProperty("ArchiveResvs", "yes")
	res.AddProperty("ArchiveSteps", "no")
	res.AddProperty("ArchiveSuspend", "no")
	res.AddProperty("ArchiveTXN", "no")
	res.AddProperty("ArchiveUsage", "no")
	res.AddProperty("PurgeEventAfter", "1month")
	res.AddProperty("PurgeJobAfter", "12month")
	res.AddProperty("PurgeResvAfter", "1month")
	res.AddProperty("PurgeStepAfter", "1month")
	res.AddProperty("PurgeSuspendAfter", "1month")
	res.AddProperty("PurgeTXNAfter", "12month")
	res.AddProperty("PurgeUsageAfter", "24month")

	return res
}
