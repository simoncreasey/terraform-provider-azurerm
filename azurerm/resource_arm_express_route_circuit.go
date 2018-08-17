package azurerm

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2018-04-01/network"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/helpers/tf"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

var expressRouteCircuitResourceName = "azurerm_express_route_circuit"

func resourceArmExpressRouteCircuit() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmExpressRouteCircuitCreateOrUpdate,
		Read:   resourceArmExpressRouteCircuitRead,
		Update: resourceArmExpressRouteCircuitCreateOrUpdate,
		Delete: resourceArmExpressRouteCircuitDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},
		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(time.Minute * 30),
			Update: schema.DefaultTimeout(time.Minute * 30),
			Delete: schema.DefaultTimeout(time.Minute * 30),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"resource_group_name": resourceGroupNameSchema(),

			"location": locationSchema(),

			"service_provider_name": {
				Type:             schema.TypeString,
				Required:         true,
				ForceNew:         true,
				DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
			},

			"peering_location": {
				Type:             schema.TypeString,
				Required:         true,
				ForceNew:         true,
				DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
			},

			"bandwidth_in_mbps": {
				Type:     schema.TypeInt,
				Required: true,
			},

			"sku": {
				Type:     schema.TypeList,
				Required: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"tier": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(network.Standard),
								string(network.Premium),
							}, true),
							DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
						},

						"family": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(network.MeteredData),
								string(network.UnlimitedData),
							}, true),
							DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
						},
					},
				},
			},

			"allow_classic_operations": {
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},

			"service_provider_provisioning_state": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"service_key": {
				Type:      schema.TypeString,
				Computed:  true,
				Sensitive: true,
			},

			"tags": tagsSchema(),
		},
	}
}

func resourceArmExpressRouteCircuitCreateOrUpdate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).expressRouteCircuitClient
	ctx := meta.(*ArmClient).StopContext

	log.Printf("[INFO] preparing arguments for Azure ARM ExpressRouteCircuit creation.")

	name := d.Get("name").(string)
	resGroup := d.Get("resource_group_name").(string)

	if d.IsNewResource() {
		// first check if there's one in this subscription requiring import
		resp, err := client.Get(ctx, resGroup, name)
		if err != nil {
			if !utils.ResponseWasNotFound(resp.Response) {
				return fmt.Errorf("Error checking for the existence of ExpressRouteCircuit %q (Resource Group %q): %+v", name, resGroup, err)
			}
		}

		if resp.ID != nil {
			return tf.ImportAsExistsError("azurerm_express_route_circuit", *resp.ID)
		}
	}

	location := azureRMNormalizeLocation(d.Get("location").(string))
	serviceProviderName := d.Get("service_provider_name").(string)
	peeringLocation := d.Get("peering_location").(string)
	bandwidthInMbps := int32(d.Get("bandwidth_in_mbps").(int))
	sku := expandExpressRouteCircuitSku(d)
	allowRdfeOps := d.Get("allow_classic_operations").(bool)
	tags := d.Get("tags").(map[string]interface{})
	expandedTags := expandTags(tags)

	erc := network.ExpressRouteCircuit{
		Name:     &name,
		Location: &location,
		Sku:      sku,
		ExpressRouteCircuitPropertiesFormat: &network.ExpressRouteCircuitPropertiesFormat{
			AllowClassicOperations: &allowRdfeOps,
			ServiceProviderProperties: &network.ExpressRouteCircuitServiceProviderProperties{
				ServiceProviderName: &serviceProviderName,
				PeeringLocation:     &peeringLocation,
				BandwidthInMbps:     &bandwidthInMbps,
			},
		},
		Tags: expandedTags,
	}

	azureRMLockByName(name, expressRouteCircuitResourceName)
	defer azureRMUnlockByName(name, expressRouteCircuitResourceName)

	future, err := client.CreateOrUpdate(ctx, resGroup, name, erc)
	if err != nil {
		return fmt.Errorf("Error Creating/Updating ExpressRouteCircuit %q (Resource Group %q): %+v", name, resGroup, err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(tf.TimeoutForCreateUpdate(d)))
	defer cancel()
	err = future.WaitForCompletionRef(waitCtx, client.Client)
	if err != nil {
		return fmt.Errorf("Error Creating/Updating ExpressRouteCircuit %q (Resource Group %q): %+v", name, resGroup, err)
	}

	read, err := client.Get(ctx, resGroup, name)
	if err != nil {
		return fmt.Errorf("Error Retrieving ExpressRouteCircuit %q (Resource Group %q): %+v", name, resGroup, err)
	}
	if read.ID == nil {
		return fmt.Errorf("Cannot read ExpressRouteCircuit %q (resource group %q) ID", name, resGroup)
	}

	d.SetId(*read.ID)

	return resourceArmExpressRouteCircuitRead(d, meta)
}

func resourceArmExpressRouteCircuitRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).expressRouteCircuitClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}

	resourceGroup := id.ResourceGroup
	name := id.Path["expressRouteCircuits"]

	resp, err := client.Get(ctx, resourceGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			log.Printf("[INFO] Express Route Circuit %q not found. Removing from state", name)
			d.SetId("")
			return nil
		}

		return fmt.Errorf("Error making Read request on Express Route Circuit %s: %+v", name, err)
	}

	d.Set("name", resp.Name)
	d.Set("resource_group_name", resourceGroup)
	if location := resp.Location; location != nil {
		d.Set("location", azureRMNormalizeLocation(*location))
	}

	if resp.Sku != nil {
		sku := flattenExpressRouteCircuitSku(resp.Sku)
		if err := d.Set("sku", sku); err != nil {
			return fmt.Errorf("Error flattening `sku`: %+v", err)
		}
	}

	if props := resp.ServiceProviderProperties; props != nil {
		d.Set("service_provider_name", props.ServiceProviderName)
		d.Set("peering_location", props.PeeringLocation)
		d.Set("bandwidth_in_mbps", props.BandwidthInMbps)
	}

	if props := resp.ExpressRouteCircuitPropertiesFormat; props != nil {
		d.Set("service_provider_provisioning_state", string(props.ServiceProviderProvisioningState))
		d.Set("service_key", props.ServiceKey)
		d.Set("allow_classic_operations", props.AllowClassicOperations)
	}

	flattenAndSetTags(d, resp.Tags)

	return nil
}

func resourceArmExpressRouteCircuitDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).expressRouteCircuitClient
	ctx := meta.(*ArmClient).StopContext

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resourceGroup := id.ResourceGroup
	name := id.Path["expressRouteCircuits"]

	azureRMLockByName(name, expressRouteCircuitResourceName)
	defer azureRMUnlockByName(name, expressRouteCircuitResourceName)

	future, err := client.Delete(ctx, resourceGroup, name)
	if err != nil {
		return err
	}

	waitCtx, cancel := context.WithTimeout(ctx, d.Timeout(schema.TimeoutDelete))
	defer cancel()
	err = future.WaitForCompletionRef(waitCtx, client.Client)
	if err != nil {
		return err
	}

	return nil
}

func expandExpressRouteCircuitSku(d *schema.ResourceData) *network.ExpressRouteCircuitSku {
	skuSettings := d.Get("sku").([]interface{})
	v := skuSettings[0].(map[string]interface{}) // [0] is guarded by MinItems in schema.
	tier := v["tier"].(string)
	family := v["family"].(string)
	name := fmt.Sprintf("%s_%s", tier, family)

	return &network.ExpressRouteCircuitSku{
		Name:   &name,
		Tier:   network.ExpressRouteCircuitSkuTier(tier),
		Family: network.ExpressRouteCircuitSkuFamily(family),
	}
}

func flattenExpressRouteCircuitSku(sku *network.ExpressRouteCircuitSku) []interface{} {
	return []interface{}{
		map[string]interface{}{
			"tier":   string(sku.Tier),
			"family": string(sku.Family),
		},
	}
}
