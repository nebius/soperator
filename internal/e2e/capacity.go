package e2e

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"

	"github.com/nebius/gosdk"
	capacityv1 "github.com/nebius/gosdk/proto/nebius/capacity/v1"
	computev1 "github.com/nebius/gosdk/proto/nebius/compute/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrInsufficientCapacity = errors.New("insufficient GPU capacity")

// errNoCapacityBlockGroup means the affinity (platform + fabric) has no capacity block group at all,
// which callers treat the same way as a group with nothing available.
var errNoCapacityBlockGroup = errors.New("no capacity block group found")

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

// affinityKey identifies the capacity block group a nodeset draws from.
type affinityKey struct {
	Platform string
	Fabric   string
}

type affinityDemand struct {
	requiredGPUs uint64
	nodesetNames []string
}

type skippedNodeSet struct {
	name   string
	reason string
}

// gpuDemands aggregates how many GPUs a profile needs per capacity block group.
// Preemptible and CPU-only nodesets don't draw from a capacity block, so they are returned as skipped instead.
func gpuDemands(profile Profile) (demands map[affinityKey]affinityDemand, skipped []skippedNodeSet) {
	demands = make(map[affinityKey]affinityDemand)

	for _, ns := range profile.NodeSets {
		if ns.Preemptible {
			skipped = append(skipped, skippedNodeSet{name: ns.Name, reason: "preemptible"})
			continue
		}

		gpuCount := parseGPUCount(ns.Preset)
		if gpuCount == 0 {
			skipped = append(skipped, skippedNodeSet{
				name:   ns.Name,
				reason: fmt.Sprintf("no GPUs in preset %q", ns.Preset),
			})
			continue
		}

		key := affinityKey{Platform: ns.Platform, Fabric: ns.InfinibandFabric}
		d := demands[key]
		d.requiredGPUs += uint64(ns.Size) * uint64(gpuCount)
		d.nodesetNames = append(d.nodesetNames, ns.Name)
		demands[key] = d
	}

	return demands, skipped
}

type cbgAvailability struct {
	cbg       *capacityv1.CapacityBlockGroup
	limit     uint64
	usage     uint64
	available uint64
}

// capacityFor looks up the capacity block group backing one affinity of a profile.
// It returns errNoCapacityBlockGroup when no such group exists.
func capacityFor(ctx context.Context, sdk *gosdk.SDK, profile Profile, key affinityKey) (cbgAvailability, error) {
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
			return cbgAvailability{}, errNoCapacityBlockGroup
		}
		return cbgAvailability{}, fmt.Errorf("get capacity block group for platform=%s fabric=%s: %w",
			key.Platform, key.Fabric, err)
	}

	cbgStatus := cbg.GetStatus()
	res := cbgAvailability{
		cbg:   cbg,
		limit: cbgStatus.GetCurrentLimit(),
		usage: cbgStatus.GetUsage(),
	}
	if res.limit > res.usage {
		res.available = res.limit - res.usage
	}

	return res, nil
}

func CheckCapacity(ctx context.Context, profile Profile) error {
	demands, skipped := gpuDemands(profile)
	for _, s := range skipped {
		log.Printf("Nodeset %q: %s, skipping capacity check", s.name, s.reason)
	}

	if len(demands) == 0 {
		log.Print("No GPU nodesets to check capacity for")
		return nil
	}

	sdk, err := newNebiusSDK(ctx, profile.NebiusProfile)
	if err != nil {
		return err
	}
	defer func() {
		_ = sdk.Close()
	}()

	var insufficient bool
	for key, d := range demands {
		res, err := capacityFor(ctx, sdk, profile, key)
		if errors.Is(err, errNoCapacityBlockGroup) {
			log.Printf("CBG platform=%s fabric=%s: nodesets=%v required=%d — no capacity block group found",
				key.Platform, key.Fabric, d.nodesetNames, d.requiredGPUs)
			insufficient = true
			continue
		}
		if err != nil {
			return err
		}

		log.Printf("CBG platform=%s fabric=%s: nodesets=%v required=%d available=%d (limit=%d usage=%d)",
			key.Platform, key.Fabric, d.nodesetNames, d.requiredGPUs, res.available, res.limit, res.usage)

		if res.available < d.requiredGPUs {
			log.Printf("CBG platform=%s fabric=%s: INSUFFICIENT CAPACITY — need %d GPUs but only %d available",
				key.Platform, key.Fabric, d.requiredGPUs, res.available)
			insufficient = true
			printResourceDetails(ctx, sdk, res.cbg)
		}
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

// hasCapacity reports whether a profile's GPU demand currently fits.
// `reserved` holds demand already claimed by other in-flight runs: those runs have picked a profile
// but their instances may not exist yet, so the capacity block usage does not  account for them.
func hasCapacity(
	ctx context.Context,
	sdk *gosdk.SDK,
	profile Profile,
	reserved map[affinityKey]uint64,
) (bool, error) {
	demands, _ := gpuDemands(profile)

	for key, d := range demands {
		res, err := capacityFor(ctx, sdk, profile, key)
		if errors.Is(err, errNoCapacityBlockGroup) {
			log.Printf("  platform=%s fabric=%s: no capacity block group found", key.Platform, key.Fabric)
			return false, nil
		}
		if err != nil {
			return false, err
		}

		required := d.requiredGPUs + reserved[key]
		log.Printf("  platform=%s fabric=%s: required=%d (%d reserved by other runs) available=%d (limit=%d usage=%d)",
			key.Platform, key.Fabric, required, reserved[key], res.available, res.limit, res.usage)

		if res.available < required {
			return false, nil
		}
	}

	return true, nil
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
