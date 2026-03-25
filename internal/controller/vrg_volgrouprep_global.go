// SPDX-FileCopyrightText: The RamenDR authors
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"fmt"
	"strings"
	"time"

	volrep "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ramendrv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	rmnutil "github.com/ramendr/ramen/internal/controller/util"
)

// VRG and DRPC label to associate them with a shared VolumeGroupReplication resource.
const GlobalVGRLabel = "ramendr.openshift.io/global-vgr"

// globalVGRSyncCheckDelay is the default interval for refreshing lastGroupSyncTime
// when the external storage provider does not report per-PVC sync times and the
// schedulingInterval is zero.
const globalVGRSyncCheckDelay = 5 * time.Minute

func (v *VRGInstance) globalVGRLabel() string {
	return v.instance.GetLabels()[GlobalVGRLabel]
}

func (v *VRGInstance) hasGlobalVGRLabel() bool {
	return v.globalVGRLabel() != ""
}

func (v *VRGInstance) globalVGRNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Namespace: RamenOperatorNamespace(),
		Name:      v.globalVGRLabel(),
	}
}

func (v *VRGInstance) isGloballyOffloadedByPeerClasses(scName string) (bool, string) {
	for idx := range v.instance.Spec.Async.PeerClasses {
		pc := &v.instance.Spec.Async.PeerClasses[idx]

		if scName == pc.StorageClassName && pc.Global {
			return true, pc.GroupReplicationID
		}
	}

	return false, ""
}

func (v *VRGInstance) addGlobalVGRLabel(grID string) error {
	// Label value doubles as the global VGR resource name, derived from GroupReplicationID.
	vgrLabel := rmnutil.GlobalVGRName(grID)

	if rmnutil.AddLabel(v.instance, GlobalVGRLabel, vgrLabel) {
		if err := v.reconciler.Update(v.ctx, v.instance); err != nil {
			return fmt.Errorf("failed to add label %s to VRG %s/%s: %w",
				GlobalVGRLabel, v.instance.Namespace, v.instance.Name, err)
		}

		v.log.Info("Added global VGR label", "label", vgrLabel)
	}

	return nil
}

func (v *VRGInstance) hasGlobalPeerClass() bool {
	for idx := range v.instance.Spec.Async.PeerClasses {
		if v.instance.Spec.Async.PeerClasses[idx].Global {
			return true
		}
	}

	return false
}

// processGloballyOffloadedPVCs validates that all offloaded PVCs in the list are
// globally offloaded, and that all PVCs share the same GroupReplicationID. If so,
// the VRG is labeled with the global VGR label.
func (v *VRGInstance) processGloballyOffloadedPVCs(pvcList *corev1.PersistentVolumeClaimList) error {
	if !v.hasGlobalPeerClass() {
		return nil
	}

	var grID string

	for idx := range pvcList.Items {
		pvc := &pvcList.Items[idx]

		pvcGlobal, pvcGRID := v.isGloballyOffloadedByPeerClasses(*pvc.Spec.StorageClassName)

		if !pvcGlobal {
			return fmt.Errorf("peerClass for StorageClass (%s) is not globally offloaded for PVC (%s/%s)",
				*pvc.Spec.StorageClassName, pvc.GetNamespace(), pvc.GetName())
		}

		if idx == 0 {
			grID = pvcGRID

			continue
		}

		if pvcGRID != grID {
			return fmt.Errorf("not all PVCs share the same GroupReplicationID, cannot use global VGR")
		}
	}

	return v.addGlobalVGRLabel(grID)
}

// isGlobalStateInConsensus checks that all VRGs sharing the same global VGR label
// have the desired replication state. This prevents any single VRG from proceeding
// until all VRGs with the same label agree.
func (v *VRGInstance) isGlobalStateInConsensus() bool {
	vgrLabel := v.globalVGRLabel()
	state := v.instance.Spec.ReplicationState
	log := v.log.WithName("GlobalStateConsensus").WithValues("label", vgrLabel, "state", state)

	var vrgs ramendrv1alpha1.VolumeReplicationGroupList

	if err := v.reconciler.List(v.ctx, &vrgs,
		client.MatchingLabels{GlobalVGRLabel: vgrLabel},
	); err != nil {
		log.Error(err, "Failed to list VRGs")

		return false
	}

	var pending []string

	for idx := range vrgs.Items {
		vrg := &vrgs.Items[idx]
		if vrg.Name == v.instance.Name && vrg.Namespace == v.instance.Namespace {
			continue
		}

		if vrg.Spec.ReplicationState != state {
			pending = append(pending, vrg.Namespace+"/"+vrg.Name)
		}
	}

	if len(pending) > 0 {
		msg := fmt.Sprintf("Pending: %s; expected state %s", strings.Join(pending, ", "), state)
		log.Info(msg)
		v.setGlobalStateCondition(false, msg)

		return false
	}

	msg := fmt.Sprintf("Consensus reached for state %s", state)
	log.Info(msg, "count", len(vrgs.Items))
	v.setGlobalStateCondition(true, msg)

	return true
}

// isGlobalDeleteInConsensus checks that all VRGs sharing the same global VGR label
// have a deletion timestamp. The global VGR is only deleted when all its VRGs are removed.
func (v *VRGInstance) isGlobalDeleteInConsensus() bool {
	vgrLabel := v.globalVGRLabel()
	log := v.log.WithName("GlobalDeleteConsensus").WithValues("label", vgrLabel)

	var vrgs ramendrv1alpha1.VolumeReplicationGroupList

	if err := v.reconciler.List(v.ctx, &vrgs,
		client.MatchingLabels{GlobalVGRLabel: vgrLabel},
	); err != nil {
		log.Error(err, "Failed to list VRGs")

		return false
	}

	for idx := range vrgs.Items {
		vrg := &vrgs.Items[idx]
		if !rmnutil.ResourceIsDeleted(vrg) {
			log.Info("Consensus not reached, VRG not yet being deleted",
				"vrg", vrg.Name, "namespace", vrg.Namespace)

			return false
		}
	}

	return true
}

func (v *VRGInstance) setGlobalStateCondition(met bool, message string) {
	status := metav1.ConditionFalse
	reason := ConditionReasonConsensusNotReached

	if met {
		status = metav1.ConditionTrue
		reason = ConditionReasonConsensusReached
	}

	rmnutil.SetStatusCondition(&v.instance.Status.Conditions, metav1.Condition{
		Type:               VRGConditionTypeGlobalState,
		Status:             status,
		ObservedGeneration: v.instance.Generation,
		Reason:             reason,
		Message:            message,
	})
}

func (v *VRGInstance) isGlobalVGRStateMatched(
	status *volrep.VolumeReplicationStatus, desiredState ramendrv1alpha1.ReplicationState,
) bool {
	switch desiredState {
	case ramendrv1alpha1.Primary:
		return status.State == volrep.PrimaryState
	case ramendrv1alpha1.Secondary:
		return status.State == volrep.SecondaryState
	default:
		return false
	}
}

// validateGlobalVGRStatus checks if the global VGR state matches the desired
// replication state and updates PVC conditions accordingly. Since the storage
// provider manages replication externally, DataProtected is assumed true for
// both primary and secondary states, and Resync/Degraded conditions are not
// checked as the storage provider does not report them.
func (v *VRGInstance) validateGlobalVGRStatus(
	volRep client.Object, pvcs []*corev1.PersistentVolumeClaim,
	status *volrep.VolumeReplicationStatus, state ramendrv1alpha1.ReplicationState,
) bool {
	if !v.isGlobalVGRStateMatched(status, state) {
		return false
	}

	dataReadyMsg := "Global VGR state matches desired replication state"
	dataProtectedMsg := "Data protection is assumed for global VGR"

	for idx := range pvcs {
		pvc := pvcs[idx]

		v.updatePVCDataReadyCondition(pvc.Namespace, pvc.Name, VRGConditionReasonReady, dataReadyMsg)
		v.updatePVCDataProtectedCondition(pvc.Namespace, pvc.Name, VRGConditionReasonDataProtected, dataProtectedMsg)
		v.updatePVCLastSyncCounters(pvc.Namespace, pvc.Name, status)
	}

	v.log.Info(dataReadyMsg, "vgr", volRep.GetName(), "namespace", volRep.GetNamespace(), "state", state)

	return true
}

// globalVGRFallbackSyncTime returns a fallback lastGroupSyncTime for global VGRs.
// Reuses the existing value if still within globalVGRSyncCheckDelay to avoid
// unnecessary API writes, otherwise returns current time.
func (v *VRGInstance) globalVGRFallbackSyncTime() *metav1.Time {
	existing := v.instance.Status.LastGroupSyncTime
	if existing != nil && time.Since(existing.Time) < globalVGRSyncCheckDelay {
		return existing
	}

	now := metav1.Now()

	v.log.Info("Global VGR: updating lastGroupSyncTime, no per PVC sync time available")

	return &now
}

// globalVGRRequeueDelay returns the remaining time until the next lastGroupSyncTime
// refresh is needed. Returns 0 if no timestamp exists yet.
func (v *VRGInstance) globalVGRRequeueDelay() time.Duration {
	existing := v.instance.Status.LastGroupSyncTime
	if existing == nil {
		return 0
	}

	return max(0, time.Until(existing.Time.Add(globalVGRSyncCheckDelay)))
}
