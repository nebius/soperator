package e2e

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"

	"github.com/nebius/gosdk"
	capacityv1 "github.com/nebius/gosdk/proto/nebius/capacity/v1"
	computev1 "github.com/nebius/gosdk/proto/nebius/compute/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrInsufficientCapacity = errors.New("insufficient GPU capacity")

var gpuCountRe = regexp.MustCompile(`^(\d+)gpu-`)

func parseGPUCount(preset string) int {
	m := gpuCountRe.FindStringSubmatch(preset)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

func CheckCapacity(ctx context.Context, profile Profile) error {
	type (
		affinityKey struct {
			Platform string
			Fabric   string
		}
		affinityDemand struct {
			required uint64
			nodesets []string
		}
	)

	token := os.Getenv("NEBIUS_IAM_TOKEN")
	if token == "" {
		log.Print("NEBIUS_IAM_TOKEN is not set, skipping capacity check")
		return nil
	}

	demands := make(map[affinityKey]affinityDemand)
	for _, ns := range profile.NodeSets {
		if ns.Preemptible {
			log.Printf("Nodeset %q: preemptible, skipping capacity check", ns.Name)
			continue
		}

		gpuCount := parseGPUCount(ns.Preset)
		if gpuCount == 0 {
			log.Printf("Nodeset %q: no GPUs in preset %q, skipping capacity check", ns.Name, ns.Preset)
			continue
		}

		key := affinityKey{Platform: ns.Platform, Fabric: ns.InfinibandFabric}
		d := demands[key]
		d.required += uint64(ns.Size) * uint64(gpuCount)
		d.nodesets = append(d.nodesets, ns.Name)
		demands[key] = d
	}

	if len(demands) == 0 {
		log.Print("No GPU nodesets to check capacity for")
		return nil
	}

	sdk, err := gosdk.New(ctx, gosdk.WithCredentials(gosdk.IAMToken(token)))
	if err != nil {
		return fmt.Errorf("create gosdk client: %w", err)
	}
	defer func() {
		_ = sdk.Close()
	}()

	var insufficient bool
	for key, d := range demands {
		cbg, err := sdk.Services().Capacity().V1().CapacityBlockGroup().GetByResourceAffinity(ctx,
			&capacityv1.GetCapacityBlockGroupByResourceAffinityRequest{
				ParentId: profile.NebiusTenantID,
				Region:   profile.NebiusRegion,
				ResourceAffinity: &capacityv1.ResourceAffinity{
					Versions: &capacityv1.ResourceAffinity_ComputeV1{
						ComputeV1: &capacityv1.ResourceAffinityComputeV1{
							Platform: key.Platform,
							Fabric:   key.Fabric,
						},
					},
				},
			},
		)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				log.Printf("CBG platform=%s fabric=%s: nodesets=%v required=%d — no capacity block group found",
					key.Platform, key.Fabric, d.nodesets, d.required)
				insufficient = true
				continue
			}
			return fmt.Errorf("get capacity block group for platform=%s fabric=%s: %w", key.Platform, key.Fabric, err)
		}

		cbgStatus := cbg.GetStatus()
		currentLimit := cbgStatus.GetCurrentLimit()
		usage := cbgStatus.GetUsage()
		var available uint64
		if currentLimit > usage {
			available = currentLimit - usage
		}

		log.Printf("CBG platform=%s fabric=%s: nodesets=%v required=%d available=%d (limit=%d usage=%d)",
			key.Platform, key.Fabric, d.nodesets, d.required, available, currentLimit, usage)

		if available >= d.required {
			continue
		}

		log.Printf("CBG platform=%s fabric=%s: INSUFFICIENT CAPACITY — need %d GPUs but only %d available",
			key.Platform, key.Fabric, d.required, available)
		insufficient = true

		printResourceDetails(ctx, sdk, cbg)
	}

	if !insufficient {
		log.Print("Capacity check passed: all capacity block groups have sufficient GPU capacity")
		return nil
	}

	if profile.CapacityStrategy == CapacityStrategyCancel {
		return ErrInsufficientCapacity
	}

	log.Print("Capacity check: insufficient capacity detected but strategy is warn, continuing")
	return nil
}

func printResourceDetails(ctx context.Context, sdk *gosdk.SDK, cbg *capacityv1.CapacityBlockGroup) {
	cbgID := cbg.GetMetadata().GetId()
	resp, err := sdk.Services().Capacity().V1().CapacityBlockGroup().ListResources(ctx,
		&capacityv1.ListCapacityBlockGroupResourcesRequest{
			Id: cbgID,
		},
	)
	if err != nil {
		log.Printf("  List resources for CBG %s: %v", cbgID, err)
		return
	}

	if len(resp.GetResourceIds()) == 0 {
		log.Printf("  CBG %s: No instances found", cbgID)
		return
	}

	log.Printf("  CBG %s: %d instances using capacity:", cbgID, len(resp.GetResourceIds()))
	for _, instanceID := range resp.GetResourceIds() {
		instance, err := sdk.Services().Compute().V1().Instance().Get(ctx,
			&computev1.GetInstanceRequest{Id: instanceID},
		)
		if err != nil {
			log.Printf("    Instance %s: Get failed: %v", instanceID, err)
			continue
		}

		meta := instance.GetMetadata()
		instanceStatus := instance.GetStatus()
		log.Printf("    instance %s: name=%s parent=%s state=%s created=%s",
			instanceID,
			meta.GetName(),
			meta.GetParentId(),
			instanceStatus.GetState().String(),
			meta.GetCreatedAt().AsTime().Format("2006-01-02 15:04:05"),
		)
	}
}
