package accounting

import (
	"errors"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/v25/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/values"
)

func RenderMariaDbGrant(
	namespace,
	clusterName string,
	accounting *values.SlurmAccounting,
) (*mariadbv1alpha1.Grant, error) {

	if !accounting.MariaDb.Enabled {
		return nil, errors.New("MariaDb is not enabled")
	}
	// mariaDb := accounting.MariaDb
	labels := common.RenderLabels(consts.ComponentTypeMariaDbOperator, clusterName)

	return &mariadbv1alpha1.Grant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      naming.BuildMariaDbName(clusterName),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: mariadbv1alpha1.GrantSpec{
			MariaDBRef: mariadbv1alpha1.MariaDBRef{
				WaitForIt: true,
				ObjectReference: mariadbv1alpha1.ObjectReference{
					Name:      naming.BuildMariaDbName(clusterName),
					Namespace: namespace,
				},
			},
			Privileges: []string{
				"ALL PRIVILEGES",
			},
			Database:    consts.MariaDbDatabase,
			Username:    consts.MariaDbUsername,
			Table:       consts.MariaDbTable,
			GrantOption: true,
			Host:        ptr.To("%"),
			SQLTemplate: mariadbv1alpha1.SQLTemplate{
				RequeueInterval: &metav1.Duration{
					Duration: 30,
				},
				RetryInterval: &metav1.Duration{
					Duration: 5,
				},
			},
		},
	}, nil
}
