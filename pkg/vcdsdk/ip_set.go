package vcdsdk

import (
    "context"
    "fmt"
    "strings"
    "time"
    "github.com/vmware/go-vcloud-director/v2/govcd"
    "github.com/vmware/go-vcloud-director/v2/types/v56"
    "k8s.io/klog"
)

func (gm *GatewayManager) CreateOrUpdateIPSet(ctx context.Context, ipSetName string, ipAddresses []string) (string, error) {
    client := gm.Client
    if client == nil || client.VCDClient == nil {
        return "", fmt.Errorf("VCD client is not initialized")
    }

    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return "", fmt.Errorf("unable to get org for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return "", fmt.Errorf("obtained nil org for name [%s]", client.ClusterOrgName)
    }

    // Check if IP Set already exists
    existingIpSet, err := org.GetNsxtFirewallGroupByName(ipSetName, types.FirewallGroupTypeIpSet)
    if err != nil && !govcd.ContainsNotFound(err) {
        return "", fmt.Errorf("error checking for existing IP Set [%s]: [%v]", ipSetName, err)
    }

    if existingIpSet != nil && existingIpSet.NsxtFirewallGroup != nil {
        // Update existing IP Set
        existingIpSet.NsxtFirewallGroup.IpAddresses = ipAddresses
        existingIpSet.NsxtFirewallGroup.Description = "IP Set for Kubernetes service load balancer"
        
        updatedIpSet, err := existingIpSet.Update(existingIpSet.NsxtFirewallGroup)
        if err != nil {
            return "", fmt.Errorf("unable to update IP Set [%s]: [%v]", ipSetName, err)
        }
        
        return updatedIpSet.NsxtFirewallGroup.ID, nil
    } else {
        // Create new IP Set
        edge, err := gm.GetNsxtEdgeGateway()
        if err != nil {
            return "", fmt.Errorf("unable to get NSX-T Edge Gateway: [%v]", err)
        }

        ipSetDefinition := &types.NsxtFirewallGroup{
            Name:        ipSetName,
            Description: "IP Set for Kubernetes service load balancer",
            Type:        types.FirewallGroupTypeIpSet,
            OwnerRef:    &types.OpenApiReference{ID: edge.EdgeGateway.ID},
            IpAddresses: ipAddresses,
        }

        createdIpSet, err := edge.CreateNsxtFirewallGroup(ipSetDefinition)
        if err != nil {
            return "", fmt.Errorf("unable to create IP Set [%s]: [%v]", ipSetName, err)
        }
        
        return createdIpSet.NsxtFirewallGroup.ID, nil
    }
}

func GetIPSetName(lbPoolName string) string {
	return fmt.Sprintf("ipset-%s", lbPoolName)
}



func (gm *GatewayManager) GetIPSetByName(ctx context.Context, ipSetName string) (*types.NsxtFirewallGroup, error) {
    client := gm.Client
    if client == nil || client.VCDClient == nil {
        return nil, fmt.Errorf("VCD client is not initialized")
    }

    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return nil, fmt.Errorf("unable to get org for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return nil, fmt.Errorf("obtained nil org for name [%s]", client.ClusterOrgName)
    }

    ipSet, err := org.GetNsxtFirewallGroupByName(ipSetName, types.FirewallGroupTypeIpSet)
    if err != nil {
        if govcd.ContainsNotFound(err) {
            return nil, govcd.ErrorEntityNotFound
        }
        return nil, fmt.Errorf("unable to get IP Set [%s]: [%v]", ipSetName, err)
    }
    
    return ipSet.NsxtFirewallGroup, nil
}

func (gm *GatewayManager) GetIPSet(ctx context.Context, ipSetID string) (*types.NsxtFirewallGroup, error) {
    client := gm.Client
    if client == nil || client.VCDClient == nil {
        return nil, fmt.Errorf("VCD client is not initialized")
    }

    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return nil, fmt.Errorf("unable to get org for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return nil, fmt.Errorf("obtained nil org for name [%s]", client.ClusterOrgName)
    }

    ipSet, err := org.GetNsxtFirewallGroupById(ipSetID)
    if err != nil {
        return nil, fmt.Errorf("unable to get IP Set with ID [%s]: [%v]", ipSetID, err)
    }
    
    return ipSet.NsxtFirewallGroup, nil
}

func (gm *GatewayManager) DeleteIPSet(ctx context.Context, ipSetName string, failIfAbsent bool) error {
    client := gm.Client
    if client == nil || client.VCDClient == nil {
        return fmt.Errorf("VCD client is not initialized")
    }

    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return fmt.Errorf("unable to get org for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return fmt.Errorf("obtained nil org for name [%s]", client.ClusterOrgName)
    }

    ipSet, err := org.GetNsxtFirewallGroupByName(ipSetName, types.FirewallGroupTypeIpSet)
    if err != nil {
        if govcd.ContainsNotFound(err) {
            if !failIfAbsent {
                klog.Warningf("IP Set %s not found when trying to deleting : %v", ipSetName, err)
                return nil
            }
            return govcd.ErrorEntityNotFound
        }
        return fmt.Errorf("error getting IP Set [%s]: [%v]", ipSetName, err)
    }
    maxRetries := 5
    retryDelay := 20 * time.Second
    var lastErr error
    
    for attempt := 1; attempt <= maxRetries; attempt++ {
        err = ipSet.Delete()
        if err == nil {
            klog.Infof("Successfully deleted IP Set [%s] on attempt %d", ipSetName, attempt)
            return nil
        }
        lastErr = err
        if strings.Contains(err.Error(), "cannot be deleted as it is in use.") {
            if attempt < maxRetries {
                klog.Warningf("IP Set [%s] is in use, retrying in %v (attempt %d/%d): %v", 
                    ipSetName, retryDelay, attempt, maxRetries, err)
                time.Sleep(retryDelay)
                continue
            }
        } else {
            // If this is not the specific error, we return without a new try
            return fmt.Errorf("unable to delete IP Set [%s]: [%v]", ipSetName, err)
        }
    }
    // All tries failed with the error 'in use'.
    return fmt.Errorf("unable to delete IP Set [%s] after %d attempts: [%v]", 
        ipSetName, maxRetries, lastErr)
}


func (gm *GatewayManager) GetNsxtEdgeGateway() (*govcd.NsxtEdgeGateway, error) {
    client := gm.Client
    if client == nil || client.VCDClient == nil {
        return nil, fmt.Errorf("VCD client is not initialized")
    }

    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return nil, fmt.Errorf("unable to get org for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return nil, fmt.Errorf("obtained nil org for name [%s]", client.ClusterOrgName)
    }

    vdc, err := org.GetVDCByName(client.ClusterOVDCName, false)
    if err != nil {
        return nil, fmt.Errorf("unable to get VDC [%s]: [%v]", client.ClusterOVDCName, err)
    }

    edge, err := vdc.GetNsxtEdgeGatewayByName(gm.GatewayRef.Name)
    if err != nil {
        return nil, fmt.Errorf("unable to get NSX-T Edge Gateway [%s]: [%v]", gm.GatewayRef.Name, err)
    }

    return edge, nil
}



