// SPDX-FileCopyrightText: The RamenDR authors
// SPDX-License-Identifier: Apache-2.0

package controllers_test

import (
	"context"
	"fmt"
	"math/rand"

	volrep "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ramendrv1alpha1 "github.com/ramendr/ramen/api/v1alpha1"
	vrgController "github.com/ramendr/ramen/internal/controller"
)

type globalVGRTest struct {
	scName        string
	vgrcName      string
	replicationID string

	vrgA *globalVRGInstance
	vrgB *globalVRGInstance

	peerClass ramendrv1alpha1.PeerClass
}

type globalVRGInstance struct {
	namespace string
	vrgName   string
	pvcNames  []types.NamespacedName
	pvNames   []string
}

func newGlobalVGRTest() *globalVGRTest {
	suffix := randomSuffix()

	scName := fmt.Sprintf("global-sc-%s", suffix)
	vgrcName := fmt.Sprintf("global-vgrc-%s", suffix)
	replID := fmt.Sprintf("repl-%s", suffix)

	return &globalVGRTest{
		scName:        scName,
		vgrcName:      vgrcName,
		replicationID: replID,
		vrgA: &globalVRGInstance{
			namespace: fmt.Sprintf("global-ns-a-%s", suffix),
			vrgName:   fmt.Sprintf("vrg-a-%s", suffix),
		},
		vrgB: &globalVRGInstance{
			namespace: fmt.Sprintf("global-ns-b-%s", suffix),
			vrgName:   fmt.Sprintf("vrg-b-%s", suffix),
		},
	}
}

func randomSuffix() string {
	b := make([]byte, 5)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))] //nolint:gosec
	}

	return string(b)
}

func (g *globalVGRTest) storageID() string {
	return storageIDs[0]
}

func (g *globalVGRTest) replID() string {
	return g.replicationID
}

func (g *globalVGRTest) scLabels() map[string]string {
	return map[string]string{
		vrgController.StorageIDLabel:          g.storageID(),
		vrgController.StorageOffloadedLabel:   "",
		vrgController.GroupReplicationIDLabel: g.replID(),
	}
}

func (g *globalVGRTest) vgrcLabels() map[string]string {
	return map[string]string{
		vrgController.StorageIDLabel:          g.storageID(),
		vrgController.ReplicationIDLabel:      g.replID(),
		vrgController.GroupReplicationIDLabel: g.replID(),
		vrgController.GlobalReplicationLabel:  "true",
		"protection":                          "ramen",
	}
}

func (g *globalVGRTest) createSC() {
	By("creating global StorageClass " + g.scName)

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   g.scName,
			Labels: g.scLabels(),
		},
		Provisioner: "manual.storage.com",
	}

	Expect(k8sClient.Create(context.TODO(), sc)).To(Succeed())
}

func (g *globalVGRTest) createVGRC() {
	By("creating global VolumeGroupReplicationClass " + g.vgrcName)

	vgrc := &volrep.VolumeGroupReplicationClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:   g.vgrcName,
			Labels: g.vgrcLabels(),
			Annotations: map[string]string{
				"replication.storage.openshift.io/is-default-class": "true",
			},
		},
		Spec: volrep.VolumeGroupReplicationClassSpec{
			Provisioner: "manual.storage.com",
			Parameters: map[string]string{
				"schedulingInterval": "0m",
			},
		},
	}

	Expect(k8sClient.Create(context.TODO(), vgrc)).To(Succeed())
}

func (g *globalVGRTest) buildPeerClass() {
	g.peerClass = ramendrv1alpha1.PeerClass{
		ReplicationID:      g.replID(),
		GroupReplicationID: g.replID(),
		StorageID:          []string{g.storageID()},
		StorageClassName:   g.scName,
		Grouping:           true,
		Offloaded:          true,
		Global:             true,
	}
}

func (g *globalVGRTest) createNamespace(ns string) {
	By("creating namespace " + ns)

	Expect(k8sClient.Create(context.TODO(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	})).To(Succeed())
}

func (g *globalVGRTest) createBoundPV(pvName, pvcName, namespace string) {
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: pvName},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/" + pvName},
			},
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Capacity:                      corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			ClaimRef:                      &corev1.ObjectReference{Namespace: namespace, Name: pvcName},
			StorageClassName:              g.scName,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
		},
	}

	Expect(k8sClient.Create(context.TODO(), pv)).To(Succeed())

	pv.Status.Phase = corev1.VolumeBound
	Expect(k8sClient.Status().Update(context.TODO(), pv)).To(Succeed())
}

func (g *globalVGRTest) createBoundPVC(pvcName, pvName, namespace string) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels:    map[string]string{"appclass": "global"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")},
			},
			VolumeName:       pvName,
			StorageClassName: &g.scName,
		},
	}

	Expect(k8sClient.Create(context.TODO(), pvc)).To(Succeed())

	pvc.Status.Phase = corev1.ClaimBound
	pvc.Status.AccessModes = pvc.Spec.AccessModes
	pvc.Status.Capacity = pvc.Spec.Resources.Requests
	Expect(k8sClient.Status().Update(context.TODO(), pvc)).To(Succeed())
}

func (g *globalVGRTest) createPVCsAndPVs(inst *globalVRGInstance) {
	const count = 2

	inst.pvcNames = nil
	inst.pvNames = nil

	for i := 0; i < count; i++ {
		pvName := fmt.Sprintf("pv-%s-%d", inst.vrgName, i)
		pvcName := fmt.Sprintf("pvc-%s-%d", inst.vrgName, i)

		By("creating PV " + pvName)
		g.createBoundPV(pvName, pvcName, inst.namespace)

		By("creating PVC " + pvcName)
		g.createBoundPVC(pvcName, pvName, inst.namespace)

		inst.pvcNames = append(inst.pvcNames, types.NamespacedName{Name: pvcName, Namespace: inst.namespace})
		inst.pvNames = append(inst.pvNames, pvName)
	}
}

func (g *globalVGRTest) createVRG(inst *globalVRGInstance) {
	By("creating VRG " + inst.vrgName + " in " + inst.namespace)

	vrg := &ramendrv1alpha1.VolumeReplicationGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inst.vrgName,
			Namespace: inst.namespace,
		},
		Spec: ramendrv1alpha1.VolumeReplicationGroupSpec{
			PVCSelector:      metav1.LabelSelector{MatchLabels: map[string]string{"appclass": "global"}},
			ReplicationState: ramendrv1alpha1.Primary,
			Async: &ramendrv1alpha1.VRGAsyncSpec{
				SchedulingInterval:       "0m",
				ReplicationClassSelector: metav1.LabelSelector{MatchLabels: map[string]string{"protection": "ramen"}},
				PeerClasses:              []ramendrv1alpha1.PeerClass{g.peerClass},
			},
			S3Profiles: []string{s3Profiles[vrgS3ProfileNumber].S3ProfileName},
		},
	}

	Expect(k8sClient.Create(context.TODO(), vrg)).To(Succeed())
}

func (g *globalVGRTest) expectedGlobalVGRName() string {
	return fmt.Sprintf("vgr-global-%s", g.replID())
}

func (g *globalVGRTest) waitForGlobalVGR() {
	By("waiting for global VGR in operator namespace " + ramenNamespace)

	Eventually(func() int {
		vgrList := &volrep.VolumeGroupReplicationList{}
		Expect(k8sClient.List(context.TODO(), vgrList,
			client.InNamespace(ramenNamespace))).To(Succeed())

		count := 0

		for idx := range vgrList.Items {
			if vgrList.Items[idx].Name == g.expectedGlobalVGRName() {
				count++
			}
		}

		return count
	}, vrgtimeout, vrginterval).Should(Equal(1),
		"waiting for global VGR %s in namespace %s", g.expectedGlobalVGRName(), ramenNamespace)
}

func (g *globalVGRTest) promoteGlobalVGR() {
	By("simulating external controller: patching global VGR to primary")

	vgrKey := types.NamespacedName{
		Name:      g.expectedGlobalVGRName(),
		Namespace: ramenNamespace,
	}

	vgr := &volrep.VolumeGroupReplication{}
	Expect(k8sClient.Get(context.TODO(), vgrKey, vgr)).To(Succeed())

	vgr.Status = volrep.VolumeGroupReplicationStatus{
		VolumeReplicationStatus: volrep.VolumeReplicationStatus{
			ObservedGeneration: vgr.Generation,
			State:              volrep.PrimaryState,
			Message:            "volume group marked primary",
		},
	}

	Expect(k8sClient.Status().Update(context.TODO(), vgr)).To(Succeed())
}

func (g *globalVGRTest) waitForVRGReady(inst *globalVRGInstance) {
	vrgKey := types.NamespacedName{Name: inst.vrgName, Namespace: inst.namespace}

	Eventually(func() bool {
		vrg := &ramendrv1alpha1.VolumeReplicationGroup{}
		if err := apiReader.Get(context.TODO(), vrgKey, vrg); err != nil {
			return false
		}

		condDataReady := findCondition(vrg.Status.Conditions, vrgController.VRGConditionTypeDataReady)

		return condDataReady != nil && condDataReady.Status == metav1.ConditionTrue
	}, vrgtimeout, vrginterval).Should(BeTrue(),
		"waiting for VRG %s to become DataReady", vrgKey)
}

func (g *globalVGRTest) waitForGlobalVGRLabel(inst *globalVRGInstance) {
	vrgKey := types.NamespacedName{Name: inst.vrgName, Namespace: inst.namespace}

	Eventually(func() string {
		vrg := &ramendrv1alpha1.VolumeReplicationGroup{}
		if err := apiReader.Get(context.TODO(), vrgKey, vrg); err != nil {
			return ""
		}

		return vrg.GetLabels()[vrgController.GlobalVGRLabel]
	}, vrgtimeout, vrginterval).Should(Equal(g.expectedGlobalVGRName()),
		"waiting for global VGR label on VRG %s", vrgKey)
}

func (g *globalVGRTest) verifyLastGroupSyncTimeSet(inst *globalVRGInstance) {
	vrgKey := types.NamespacedName{Name: inst.vrgName, Namespace: inst.namespace}

	Eventually(func() bool {
		vrg := &ramendrv1alpha1.VolumeReplicationGroup{}
		if err := apiReader.Get(context.TODO(), vrgKey, vrg); err != nil {
			return false
		}

		return vrg.Status.LastGroupSyncTime != nil && !vrg.Status.LastGroupSyncTime.IsZero()
	}, vrgtimeout, vrginterval).Should(BeTrue(),
		"waiting for LastGroupSyncTime to be set on VRG %s", vrgKey)
}

func (g *globalVGRTest) verifyGlobalStateCondition(
	inst *globalVRGInstance, expectedStatus metav1.ConditionStatus,
) {
	vrgKey := types.NamespacedName{Name: inst.vrgName, Namespace: inst.namespace}

	Eventually(func() metav1.ConditionStatus {
		vrg := &ramendrv1alpha1.VolumeReplicationGroup{}
		if err := apiReader.Get(context.TODO(), vrgKey, vrg); err != nil {
			return metav1.ConditionUnknown
		}

		cond := findCondition(vrg.Status.Conditions, vrgController.VRGConditionTypeGlobalState)
		if cond == nil {
			return metav1.ConditionUnknown
		}

		return cond.Status
	}, vrgtimeout, vrginterval).Should(Equal(expectedStatus),
		"waiting for GlobalState=%s on VRG %s", expectedStatus, vrgKey)
}

func (g *globalVGRTest) updateVRGState(inst *globalVRGInstance, state ramendrv1alpha1.ReplicationState) {
	By(fmt.Sprintf("updating VRG %s/%s to %s", inst.namespace, inst.vrgName, state))

	vrgKey := types.NamespacedName{Name: inst.vrgName, Namespace: inst.namespace}

	Eventually(func() error {
		vrg := &ramendrv1alpha1.VolumeReplicationGroup{}
		if err := k8sClient.Get(context.TODO(), vrgKey, vrg); err != nil {
			return err
		}

		vrg.Spec.ReplicationState = state

		return k8sClient.Update(context.TODO(), vrg)
	}, vrgtimeout, vrginterval).Should(Succeed(),
		"failed to update VRG %s to %s", vrgKey, state)
}

func (g *globalVGRTest) deleteVRG(inst *globalVRGInstance) {
	By("deleting VRG " + inst.vrgName + " in " + inst.namespace)

	vrgKey := types.NamespacedName{Name: inst.vrgName, Namespace: inst.namespace}
	vrg := &ramendrv1alpha1.VolumeReplicationGroup{}

	if err := k8sClient.Get(context.TODO(), vrgKey, vrg); err != nil {
		return
	}

	Expect(k8sClient.Delete(context.TODO(), vrg)).To(Succeed())
}

func (g *globalVGRTest) verifyGlobalVGRExists() {
	vgrKey := types.NamespacedName{
		Name:      g.expectedGlobalVGRName(),
		Namespace: ramenNamespace,
	}

	Consistently(func() error {
		return apiReader.Get(context.TODO(), vgrKey, &volrep.VolumeGroupReplication{})
	}, vrgtimeout, vrginterval).Should(Succeed(),
		"global VGR %s should still exist", vgrKey)
}

func (g *globalVGRTest) verifyGlobalVGRDeleted() {
	vgrKey := types.NamespacedName{
		Name:      g.expectedGlobalVGRName(),
		Namespace: ramenNamespace,
	}

	Eventually(func() bool {
		err := apiReader.Get(context.TODO(), vgrKey, &volrep.VolumeGroupReplication{})

		return err != nil
	}, vrgtimeout*2, vrginterval).Should(BeTrue(),
		"global VGR %s should be deleted", vgrKey)
}

func (g *globalVGRTest) cleanupAll() {
	g.deleteVRG(g.vrgA)
	g.deleteVRG(g.vrgB)

	sc := &storagev1.StorageClass{}
	if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: g.scName}, sc); err == nil {
		Expect(k8sClient.Delete(context.TODO(), sc)).To(Succeed())
	}

	vgrc := &volrep.VolumeGroupReplicationClass{}
	if err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: g.vgrcName}, vgrc); err == nil {
		Expect(k8sClient.Delete(context.TODO(), vgrc)).To(Succeed())
	}
}

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for idx := range conditions {
		if conditions[idx].Type == condType {
			return &conditions[idx]
		}
	}

	return nil
}

var _ = Describe("VolumeReplicationGroupGlobalVGR", func() {
	var g *globalVGRTest

	Context("global VGR with two VRGs in primary state", Ordered, func() {
		It("sets up shared StorageClass, VGRC, and PeerClass", func() {
			g = newGlobalVGRTest()
			g.createSC()
			g.createVGRC()
			g.buildPeerClass()
		})

		It("creates two VRGs in separate namespaces with PVCs", func() {
			g.createNamespace(g.vrgA.namespace)
			g.createPVCsAndPVs(g.vrgA)
			g.createVRG(g.vrgA)

			g.createNamespace(g.vrgB.namespace)
			g.createPVCsAndPVs(g.vrgB)
			g.createVRG(g.vrgB)
		})

		It("adds the global VGR label to both VRGs", func() {
			g.waitForGlobalVGRLabel(g.vrgA)
			g.waitForGlobalVGRLabel(g.vrgB)
		})

		It("creates a single VGR in the operator namespace", func() {
			g.waitForGlobalVGR()
		})

		It("reaches consensus and becomes ready after VGR is promoted", func() {
			g.promoteGlobalVGR()
			g.verifyGlobalStateCondition(g.vrgA, metav1.ConditionTrue)
			g.verifyGlobalStateCondition(g.vrgB, metav1.ConditionTrue)
			g.waitForVRGReady(g.vrgA)
			g.waitForVRGReady(g.vrgB)
		})

		It("sets LastGroupSyncTime as fallback when external storage does not provide it", func() {
			g.verifyLastGroupSyncTimeSet(g.vrgA)
			g.verifyLastGroupSyncTimeSet(g.vrgB)
		})

		AfterAll(func() {
			g.cleanupAll()
		})
	})

	Context("global VGR consensus blocks when VRGs disagree", Ordered, func() {
		It("sets up shared resources", func() {
			g = newGlobalVGRTest()
			g.createSC()
			g.createVGRC()
			g.buildPeerClass()
		})

		It("creates two primary VRGs and promotes VGR", func() {
			g.createNamespace(g.vrgA.namespace)
			g.createPVCsAndPVs(g.vrgA)
			g.createVRG(g.vrgA)

			g.createNamespace(g.vrgB.namespace)
			g.createPVCsAndPVs(g.vrgB)
			g.createVRG(g.vrgB)

			g.waitForGlobalVGRLabel(g.vrgA)
			g.waitForGlobalVGRLabel(g.vrgB)
			g.waitForGlobalVGR()
			g.promoteGlobalVGR()
			g.waitForVRGReady(g.vrgA)
			g.waitForVRGReady(g.vrgB)
		})

		It("blocks VRG-A when only VRG-A transitions to secondary", func() {
			g.updateVRGState(g.vrgA, ramendrv1alpha1.Secondary)
			g.verifyGlobalStateCondition(g.vrgA, metav1.ConditionFalse)
		})

		It("reaches consensus once VRG-B also transitions to secondary", func() {
			g.updateVRGState(g.vrgB, ramendrv1alpha1.Secondary)
			g.verifyGlobalStateCondition(g.vrgA, metav1.ConditionTrue)
			g.verifyGlobalStateCondition(g.vrgB, metav1.ConditionTrue)
		})

		AfterAll(func() {
			g.cleanupAll()
		})
	})

	Context("global VGR deletion consensus", Ordered, func() {
		It("sets up shared resources", func() {
			g = newGlobalVGRTest()
			g.createSC()
			g.createVGRC()
			g.buildPeerClass()
		})

		It("creates two primary VRGs and promotes VGR", func() {
			g.createNamespace(g.vrgA.namespace)
			g.createPVCsAndPVs(g.vrgA)
			g.createVRG(g.vrgA)

			g.createNamespace(g.vrgB.namespace)
			g.createPVCsAndPVs(g.vrgB)
			g.createVRG(g.vrgB)

			g.waitForGlobalVGRLabel(g.vrgA)
			g.waitForGlobalVGRLabel(g.vrgB)
			g.waitForGlobalVGR()
			g.promoteGlobalVGR()
			g.waitForVRGReady(g.vrgA)
			g.waitForVRGReady(g.vrgB)
		})

		It("keeps global VGR alive when only one VRG is deleted", func() {
			g.deleteVRG(g.vrgA)
			g.verifyGlobalVGRExists()
		})

		It("deletes global VGR once all VRGs are deleted", func() {
			g.deleteVRG(g.vrgB)
			g.verifyGlobalVGRDeleted()
		})

		AfterAll(func() {
			g.cleanupAll()
		})
	})
})
