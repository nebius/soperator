package loginupdatecontroller

import (
	"context"
	"errors"
	"testing"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	"nebius.ai/slurm-operator/internal/consts"
)

func TestReconcileMetricsPreservePersistedScheduleAfterPatchFailure(t *testing.T) {
	for _, tt := range []struct {
		name        string
		announcedAt string
		scheduledAt string
		wantState   int
	}{
		{name: "missing schedule", wantState: podStateAwaitingObservation},
		{
			name:        "recalculated schedule",
			announcedAt: testNoticeTime.Add(-48 * time.Hour).Format(time.RFC3339Nano),
			scheduledAt: testNoticeTime.Add(-time.Hour).Format(time.RFC3339Nano),
			wantState:   podStateRestartEligible,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, sts, pods := testLoginReconciler(t, func(sts *kruisev1b1.StatefulSet, pods []*corev1.Pod) []*corev1.Pod {
				sts.Spec.Replicas = ptr.To(int32(1))
				if tt.announcedAt != "" {
					pods[0].Annotations = map[string]string{
						consts.AnnotationLoginUpdateAnnouncedAt: tt.announcedAt,
						consts.AnnotationLoginUpdateScheduledAt: tt.scheduledAt,
					}
				}
				return pods[:1]
			})
			r.metrics = newRolloutMetrics(prometheus.NewRegistry())
			failPatch := true
			r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{
				Patch: func(ctx context.Context, c client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*corev1.Pod); ok && failPatch {
						return errors.New("injected schedule patch failure")
					}
					return c.Patch(ctx, obj, patch, opts...)
				},
			})
			gaugeValue := func(gauge prometheus.Gauge) float64 {
				t.Helper()
				var sample dto.Metric
				require.NoError(t, gauge.Write(&sample))
				return sample.GetGauge().GetValue()
			}
			assertPublishedSchedule := func(pod *corev1.Pod, wantState int, wantError float64) {
				t.Helper()
				var deadline float64
				if scheduled := pod.Annotations[consts.AnnotationLoginUpdateScheduledAt]; scheduled != "" {
					parsed, err := time.Parse(time.RFC3339Nano, scheduled)
					require.NoError(t, err)
					deadline = float64(parsed.Unix())
				}
				labels := rolloutLabels(sts.Namespace, sts.Labels[consts.LabelInstanceKey])
				assert.Equal(t, deadline, gaugeValue(r.metrics.nextRestart.With(labels)), "export only the persisted restart deadline")
				labels["login_pod"] = pod.Name
				assert.Equal(t, float64(wantState), gaugeValue(r.metrics.podState.With(labels)))
				delete(labels, "login_pod")
				labels["reason"] = waitError
				assert.Equal(t, wantError, gaugeValue(r.metrics.waiting.With(labels)))
			}

			req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)}
			_, err := r.Reconcile(t.Context(), req)
			require.ErrorContains(t, err, "injected schedule patch failure")
			persisted := getLoginPod(t, r.Client, pods[0])
			require.Equal(t, tt.scheduledAt, persisted.Annotations[consts.AnnotationLoginUpdateScheduledAt])
			assertPublishedSchedule(persisted, tt.wantState, 1)

			failPatch = false
			_, err = r.Reconcile(t.Context(), req)
			require.NoError(t, err)
			persisted = getLoginPod(t, r.Client, pods[0])
			require.NotEqual(t, tt.scheduledAt, persisted.Annotations[consts.AnnotationLoginUpdateScheduledAt])
			assertPublishedSchedule(persisted, podStateNoticePeriod, 0)
		})
	}
}
