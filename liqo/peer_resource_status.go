// Copyright 2019-2025 The Liqo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package liqo

import (
	"context"
	"fmt"
	execCmd "os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Constants for peer status values.
const (
	statusNoPeers = "no_peers"
)

// LiqoctlInfoOutput represents the structure of liqoctl info -o yaml output.
type LiqoctlInfoOutput struct {
	Health struct {
		Healthy bool `yaml:"healthy"`
	} `yaml:"health"`
	Local struct {
		APIServerAddr string `yaml:"APIServerAddr"`
		ClusterID     string `yaml:"clusterID"`
		Version       string `yaml:"version"`
	} `yaml:"local"`
	Peerings struct {
		Peers []PeerInfo `yaml:"peers"`
	} `yaml:"peerings"`
}

// PeerInfo represents information about a peer cluster.
type PeerInfo struct {
	AuthenticationStatus string `yaml:"authenticationStatus"`
	ClusterID            string `yaml:"clusterID"`
	NetworkingStatus     string `yaml:"networkingStatus"`
	OffloadingStatus     string `yaml:"offloadingStatus"`
	Role                 string `yaml:"role"`
}

// getRemoteClusterID gets the cluster ID from the remote cluster.
func getRemoteClusterID(ctx context.Context, liqoctlPath, remoteKubeconfig string, plan *peerResourceModel) (string, error) {
	args := []string{"info"}

	// Add remote kubeconfig
	if remoteKubeconfig != "" {
		args = append(args, "--kubeconfig", remoteKubeconfig)
	}

	// Add remote Liqo namespace if specified
	if !plan.RemoteLiqoNamespace.IsNull() && plan.RemoteLiqoNamespace.ValueString() != "" {
		args = append(args, "--namespace", plan.RemoteLiqoNamespace.ValueString())
	}

	// Use YAML output format
	args = append(args, "-o", "yaml")

	// Execute liqoctl info on remote cluster
	cmd := execCmd.CommandContext(ctx, liqoctlPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("liqoctl info command failed on remote cluster: %s\nOutput: %s", err.Error(), string(output))
	}

	// Parse YAML output
	var remoteInfo LiqoctlInfoOutput
	err = yaml.Unmarshal(output, &remoteInfo)
	if err != nil {
		return "", fmt.Errorf("failed to parse remote cluster info YAML: %s", err.Error())
	}

	return remoteInfo.Local.ClusterID, nil
}

// checkPeeringStatus uses liqoctl info to check the status of peering for a specific remote cluster.
func checkPeeringStatus(ctx context.Context, liqoctlPath, localKubeconfig string, plan *peerResourceModel) (string, error) {
	// Step 1: Get the remote cluster ID
	remoteClusterID, err := getRemoteClusterID(ctx, liqoctlPath, plan.RemoteKubeconfig.ValueString(), plan)
	if err != nil {
		return statusError, fmt.Errorf("failed to get remote cluster ID: %s", err.Error())
	}

	if remoteClusterID == "" {
		return statusError, fmt.Errorf("remote cluster ID is empty")
	}

	// Step 2: Get local cluster's peering information
	args := []string{"info"}

	// Add local kubeconfig if specified
	if localKubeconfig != "" {
		args = append(args, "--kubeconfig", localKubeconfig)
	}

	// Add Liqo namespace if specified
	if !plan.LiqoNamespace.IsNull() && plan.LiqoNamespace.ValueString() != "" {
		args = append(args, "--namespace", plan.LiqoNamespace.ValueString())
	}

	// Use YAML output format
	args = append(args, "-o", "yaml")

	// Execute liqoctl info on local cluster
	cmd := execCmd.CommandContext(ctx, liqoctlPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return statusError, fmt.Errorf("liqoctl info command failed on local cluster: %s\nOutput: %s", err.Error(), string(output))
	}

	// Parse YAML output
	var localInfo LiqoctlInfoOutput
	err = yaml.Unmarshal(output, &localInfo)
	if err != nil {
		return statusError, fmt.Errorf("failed to parse local cluster info YAML: %s", err.Error())
	}

	// Step 3: Look for the specific remote cluster in the peer list
	var targetPeer *PeerInfo
	for i, peer := range localInfo.Peerings.Peers {
		if peer.ClusterID == remoteClusterID {
			targetPeer = &localInfo.Peerings.Peers[i]
			break
		}
	}

	// Check if we found the target peer
	if targetPeer == nil {
		return statusNoPeers, fmt.Errorf("remote cluster %s not found in local cluster's peer list", remoteClusterID)
	}

	// Step 4: Determine the peering status for this specific cluster
	return evaluatePeerStatus(targetPeer), nil
}

// evaluatePeerStatus determines the overall status of a specific peer cluster.
func evaluatePeerStatus(peer *PeerInfo) string {
	nHealthy := 0
	nUnhealthy := 0
	nDisabled := 0

	countStatus := func(status string) {
		switch {
		case strings.Contains(status, "Unhealthy"):
			nUnhealthy++
		case strings.Contains(status, "Disabled"):
			nDisabled++
		case strings.Contains(status, "Healthy"):
			nHealthy++
		}
	}

	countStatus(peer.AuthenticationStatus)
	countStatus(peer.NetworkingStatus)
	countStatus(peer.OffloadingStatus)

	if nUnhealthy > 0 {
		return statusEstablishing
	}
	if nHealthy > 0 {
		return statusReady
	}
	return statusEstablishing
}

// waitForPeeringCompletion polls the peering status until it's ready or fails.
func waitForPeeringCompletion(
	ctx context.Context,
	liqoctlPath, localKubeconfig string,
	plan *peerResourceModel,
	timeout time.Duration,
) (string, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second) // Poll every 10 seconds
	defer ticker.Stop()

	// Initial check
	status, err := checkPeeringStatus(timeoutCtx, liqoctlPath, localKubeconfig, plan)
	if err != nil {
		return status, err
	}
	if status == statusReady {
		return status, nil
	}

	for {
		select {
		case <-timeoutCtx.Done():
			return "timeout", fmt.Errorf("timeout waiting for peering completion after %v", timeout)
		case <-ticker.C:
			status, err := checkPeeringStatus(timeoutCtx, liqoctlPath, localKubeconfig, plan)
			if err != nil {
				// Log error but continue polling unless it's a fatal error
				if strings.Contains(err.Error(), "failed") || strings.Contains(err.Error(), "error") {
					return statusError, err
				}
				// Continue polling for other errors (might be temporary)
				continue
			}

			switch status {
			case "ready":
				return status, nil
			case "error":
				return status, fmt.Errorf("peering failed")
			case "establishing", "no_peers":
				// Continue polling
				continue
			default:
				// Unknown status, continue polling
				continue
			}
		}
	}
}
