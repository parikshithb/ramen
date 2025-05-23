// SPDX-FileCopyrightText: The RamenDR authors
// SPDX-License-Identifier: Apache-2.0

package dractions

import (
	"fmt"

	ramen "github.com/ramendr/ramen/api/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ramendr/ramen/e2e/types"
	"github.com/ramendr/ramen/e2e/util"
)

func waitDRPCReady(ctx types.TestContext, namespace string, drpcName string) error {
	log := ctx.Logger()
	hub := ctx.Env().Hub

	log.Debugf("Waiting until drpc \"%s/%s\" is ready in cluster %q", namespace, drpcName, hub.Name)

	for {
		drpc, err := getDRPC(ctx, namespace, drpcName)
		if err != nil {
			return err
		}

		available := conditionMet(drpc.Status.Conditions, ramen.ConditionAvailable)
		peerReady := conditionMet(drpc.Status.Conditions, ramen.ConditionPeerReady)

		// Not sure if checking for progression completed is needed.
		// Ideally, conditions should be enough.
		// see https://github.com/RamenDR/ramen/issues/1988
		progressionCompleted := drpc.Status.Progression == ramen.ProgressionCompleted

		if available &&
			peerReady &&
			progressionCompleted &&
			drpc.Status.LastGroupSyncTime != nil {
			log.Debugf("drpc \"%s/%s\" is ready in cluster %q", namespace, drpcName, hub.Name)

			return nil
		}

		if err := util.Sleep(ctx.Context(), util.RetryInterval); err != nil {
			return fmt.Errorf("drpc not ready in cluster %q"+
				" (Available: %v, PeerReady: %v, ProgressionCompleted: %v, lastGroupSyncTime: %v): %w",
				hub.Name, available, peerReady, progressionCompleted, drpc.Status.LastGroupSyncTime, err)
		}
	}
}

func conditionMet(conditions []metav1.Condition, conditionType string) bool {
	condition := meta.FindStatusCondition(conditions, conditionType)

	return condition != nil && condition.Status == "True"
}

func waitDRPCPhase(ctx types.TestContext, namespace, name string, phase ramen.DRState) error {
	log := ctx.Logger()
	hub := ctx.Env().Hub

	log.Debugf("Waiting until drpc \"%s/%s\" reach phase %q in cluster %q", namespace, name, phase, hub.Name)

	for {
		drpc, err := getDRPC(ctx, namespace, name)
		if err != nil {
			return err
		}

		currentPhase := drpc.Status.Phase
		if currentPhase == phase {
			log.Debugf("drpc \"%s/%s\" phase is %q in cluster %q", namespace, name, phase, hub.Name)

			return nil
		}

		if err := util.Sleep(ctx.Context(), util.RetryInterval); err != nil {
			return fmt.Errorf("drpc %q phase is not %q in cluster %q: %w", name, phase, hub.Name, err)
		}
	}
}

func getTargetCluster(
	ctx types.TestContext,
	cluster types.Cluster,
	drPolicyName, currentCluster string,
) (string, error) {
	drpolicy, err := util.GetDRPolicy(ctx, cluster, drPolicyName)
	if err != nil {
		return "", err
	}

	var targetCluster string
	if currentCluster == drpolicy.Spec.DRClusters[0] {
		targetCluster = drpolicy.Spec.DRClusters[1]
	} else {
		targetCluster = drpolicy.Spec.DRClusters[0]
	}

	return targetCluster, nil
}

func waitDRPCDeleted(ctx types.TestContext, namespace string, name string) error {
	log := ctx.Logger()
	hub := ctx.Env().Hub

	log.Debugf("Waiting until drpc \"%s/%s\" is deleted in cluster %q", namespace, name, hub.Name)

	for {
		_, err := getDRPC(ctx, namespace, name)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.Debugf("drpc \"%s/%s\" is deleted in cluster %q", namespace, name, hub.Name)

				return nil
			}

			log.Debugf("Failed to get drpc \"%s/%s\" in cluster %q: %s", namespace, name, hub.Name, err)
		}

		if err := util.Sleep(ctx.Context(), util.RetryInterval); err != nil {
			return fmt.Errorf("drpc %q is not deleted in cluster %q: %w", name, hub.Name, err)
		}
	}
}

// nolint:unparam
func waitDRPCProgression(
	ctx types.TestContext,
	namespace, name string,
	progression ramen.ProgressionStatus,
) error {
	log := ctx.Logger()
	hub := ctx.Env().Hub

	log.Debugf("Waiting until drpc \"%s/%s\" reach progression %q in cluster %q",
		namespace, name, progression, hub.Name)

	for {
		drpc, err := getDRPC(ctx, namespace, name)
		if err != nil {
			return err
		}

		currentProgression := drpc.Status.Progression
		if currentProgression == progression {
			log.Debugf("drpc \"%s/%s\" progression is %q in cluster %q", namespace, name, progression, hub.Name)

			return nil
		}

		if err := util.Sleep(ctx.Context(), util.RetryInterval); err != nil {
			return fmt.Errorf("drpc %q progression is not %q in cluster %q: %w",
				name, progression, hub.Name, err)
		}
	}
}
