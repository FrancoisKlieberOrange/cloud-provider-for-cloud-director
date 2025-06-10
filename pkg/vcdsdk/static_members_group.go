package vcdsdk

import (
    "context"
    "fmt"
    "net/http"
    "github.com/vmware/cloud-provider-for-cloud-director/pkg/util"
    swaggerClient "github.com/vmware/cloud-provider-for-cloud-director/pkg/vcdswaggerclient_37_2"
    "github.com/vmware/go-vcloud-director/v2/govcd"
    "k8s.io/klog/v2"
)

const (
    // FirewallGroupTypeStaticMembers représente le type de groupe de pare-feu pour les membres statiques
    FirewallGroupTypeStaticMembers = "STATIC_MEMBERS"
)

// CreateOrUpdateStaticMembersGroup crée ou met à jour un groupe de pare-feu avec des membres statiques
func (gm *GatewayManager) CreateOrUpdateStaticMembersGroup(ctx context.Context, groupName string, 
    vmIDs []string) (string, error) {
    
    client := gm.Client
    if gm.GatewayRef == nil {
        return "", fmt.Errorf("gateway reference should not be nil")
    }
    
    // Vérifier si le groupe existe déjà
    existingGroup, err := gm.GetStaticMembersGroupByName(ctx, groupName)
    if err != nil && !util.IsNotFoundError(err) {
        return "", fmt.Errorf("error checking if static members group [%s] exists: [%v]", groupName, err)
    }
    
    // Créer les membres du groupe
    members := make([]swaggerClient.FirewallGroupMember, 0, len(vmIDs))
    for _, vmID := range vmIDs {
        members = append(members, swaggerClient.FirewallGroupMember{
            MemberType: "VM",
            MemberID:   vmID,
        })
    }
    
    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return "", fmt.Errorf("error getting org by name for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return "", fmt.Errorf("obtained nil org when getting org by name [%s]", client.ClusterOrgName)
    }
    
    // Si le groupe existe, le mettre à jour
    if existingGroup != nil {
        // Mettre à jour le groupe existant
        existingGroup.Members = members
        
        resp, err := client.APIClient.FirewallGroupsApi.UpdateFirewallGroup(
            ctx, *existingGroup, existingGroup.ID, org.Org.ID, nil)
        if err != nil {
            return "", fmt.Errorf("error updating static members group [%s]: [%v]", groupName, err)
        }
        if resp.StatusCode != http.StatusAccepted {
            return "", fmt.Errorf("unable to update static members group; expected http response [%v], obtained [%v]",
                http.StatusAccepted, resp.StatusCode)
        }
        
        taskURL := resp.Header.Get("Location")
        task := govcd.NewTask(&client.VCDClient.Client)
        task.Task.HREF = taskURL
        if err = task.WaitTaskCompletion(); err != nil {
            return "", fmt.Errorf("unable to update static members group; update task [%s] did not complete: [%v]",
                taskURL, err)
        }
        
        klog.Infof("Updated static members group [%s]\n", groupName)
        return existingGroup.ID, nil
    }
    
    // Créer un nouveau groupe
    newGroup := swaggerClient.FirewallGroup{
        Name:     groupName,
        Type:     FirewallGroupTypeStaticMembers,
        Members:  members,
        OwnerRef: gm.GatewayRef,
    }
    
    createdGroup, resp, err := client.APIClient.FirewallGroupsApi.CreateFirewallGroup(
        ctx, newGroup, org.Org.ID, nil)
    if err != nil {
        return "", fmt.Errorf("error creating static members group [%s]: [%v]", groupName, err)
    }
    if resp.StatusCode != http.StatusAccepted {
        return "", fmt.Errorf("unable to create static members group; expected http response [%v], obtained [%v]",
            http.StatusAccepted, resp.StatusCode)
    }
    
    taskURL := resp.Header.Get("Location")
    task := govcd.NewTask(&client.VCDClient.Client)
    task.Task.HREF = taskURL
    if err = task.WaitTaskCompletion(); err != nil {
        return "", fmt.Errorf("unable to create static members group; creation task [%s] did not complete: [%v]",
            taskURL, err)
    }
    
    klog.Infof("Created static members group [%s]\n", groupName)
    return createdGroup.ID, nil
}

// GetStaticMembersGroupByName récupère un groupe de pare-feu avec des membres statiques par son nom
func (gm *GatewayManager) GetStaticMembersGroupByName(ctx context.Context, groupName string) (*swaggerClient.FirewallGroup, error) {
    client := gm.Client
    if gm.GatewayRef == nil {
        return nil, fmt.Errorf("gateway reference should not be nil")
    }
    
    org, err := client.VCDClient.GetOrgByName(client.ClusterOrgName)
    if err != nil {
        return nil, fmt.Errorf("error getting org by name for org [%s]: [%v]", client.ClusterOrgName, err)
    }
    if org == nil || org.Org == nil {
        return nil, fmt.Errorf("obtained nil org when getting org by name [%s]", client.ClusterOrgName)
    }
    
    // Récupérer tous les groupes de pare-feu pour cette passerelle
    queryParams := map[string]interface{}{
        "filter": fmt.Sprintf("typeValue==%s;ownerRef.id==%s", FirewallGroupTypeStaticMembers, gm.GatewayRef.ID),
    }
    
    groups, resp, err