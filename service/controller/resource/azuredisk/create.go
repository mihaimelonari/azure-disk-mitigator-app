package azuredisk

import (
	"context"
	"errors"
	"os"
	"regexp"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/giantswarm/microerror"

	"github.com/giantswarm/azure-disk-mitigator/service/controller/key"
)

var (
	detachErrorRegex = regexp.MustCompile("AttachVolume\\.Attach failed for volume \"(.*?)\" : disk\\(\\/subscriptions\\/(.*?)\\/resourceGroups\\/(.*?)\\/providers\\/Microsoft\\.Compute\\/disks\\/(.*?)\\) already attached to node\\(\\/subscriptions\\/(.*?)\\/resourceGroups\\/(.*?)\\/providers\\/Microsoft\\.Compute\\/virtualMachineScaleSets\\/(.*?)\\/virtualMachines\\/(.*?)\\), could not be attached to node\\((.*?)\\)")
)

func (r *Resource) EnsureCreated(ctx context.Context, obj interface{}) error {
	event, err := key.ToEvent(obj)
	if err != nil {
		return microerror.Mask(err)
	}

	r.logger.LogCtx(ctx, "message", event.Message)
	match := detachErrorRegex.FindStringSubmatch(event.Message)
	pvcName := match[1]
	r.logger.LogCtx(ctx, "pvc", pvcName)
	// subscriptionId := match[2]
	resourceGroup := match[3]
	diskName := match[4]
	// subscriptionId := match[5]
	// resourceGroup := match[6]
	vmssName := match[7]
	vmssInstanceName := match[8]
	// nodeName := match[9]

	client, err := getVMSSClient()
	if err != nil {
		return microerror.Mask(err)
	}

	vmss, err := client.Get(ctx, resourceGroup, vmssName, vmssInstanceName, "")
	if err != nil {
		return microerror.Mask(err)
	}

	index := -1
	for i, disk := range *vmss.StorageProfile.DataDisks {
		if *disk.Name == diskName {
			index = i
		}
	}

	if index == -1 {
		return microerror.Mask(errors.New("disk not found"))
	}

	*vmss.StorageProfile.DataDisks = remove(*vmss.StorageProfile.DataDisks, index)
	//vmss.StorageProfile.DataDisks[index] = compute.DataDisk{}

	_, err = client.Update(ctx, resourceGroup, vmssName, vmssInstanceName, vmss)
	if err != nil {
		return microerror.Mask(err)
	}

	return nil
}

func getVMSSClient() (compute.VirtualMachineScaleSetVMsClient, error) {
	azureClientID := os.Getenv("AZURE_CLIENTID")
	azureClientSecret := os.Getenv("AZURE_CLIENTSECRET")
	azureSubscriptionID := os.Getenv("AZURE_SUBSCRIPTIONID")
	azureTenantID := os.Getenv("AZURE_TENANTID")

	env, err := azure.EnvironmentFromName(azure.PublicCloud.Name)
	if err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}

	oauthConfig, err := adal.NewOAuthConfig(env.ActiveDirectoryEndpoint, azureTenantID)
	if err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}

	servicePrincipalToken, err := adal.NewServicePrincipalToken(*oauthConfig, azureClientID, azureClientSecret, env.ServiceManagementEndpoint)
	if err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}

	virtualMachineScaleSetsClient := compute.NewVirtualMachineScaleSetVMsClient(azureSubscriptionID)
	virtualMachineScaleSetsClient.Authorizer = autorest.NewBearerAuthorizer(servicePrincipalToken)

	return virtualMachineScaleSetsClient, nil
}

func remove(slice []compute.DataDisk, s int) []compute.DataDisk {
	return append(slice[:s], slice[s+1:]...)
}