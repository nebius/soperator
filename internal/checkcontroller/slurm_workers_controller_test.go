package checkcontroller

// import (
// 	"context"
// 	"reflect"
// 	"testing"

// 	"k8s.io/apimachinery/pkg/types"
// 	"nebius.ai/slurm-operator/internal/slurmapi"
// 	slurmapifake "nebius.ai/slurm-operator/internal/slurmapi/fake"
// 	"sigs.k8s.io/controller-runtime/pkg/client"
// )

// func Test_slurmWorkersController_findDegradedNodes(t *testing.T) {
// 	type fields struct {
// 		Client          client.Client
// 		slurmAPIClients map[types.NamespacedName]slurmapi.Client
// 	}
// 	type args struct {
// 		ctx context.Context
// 	}
// 	tests := []struct {
// 		name    string
// 		fields  fields
// 		args    args
// 		want    map[types.NamespacedName][]slurmapi.Node
// 		wantErr bool
// 	}{
// 		{
// 			name: "happy-path",
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			apiClient := slurmapifake.NewMockClient(t)
// 			apiClient.On("").Return()

// 			c := &slurmWorkersController{
// 				Client:          tt.fields.Client,
// 				slurmAPIClients: tt.fields.slurmAPIClients,
// 			}
// 			got, err := c.findDegradedNodes(tt.args.ctx)
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("slurmWorkersController.findDegradedNodes() error = %v, wantErr %v", err, tt.wantErr)
// 				return
// 			}
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("slurmWorkersController.findDegradedNodes() = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }
