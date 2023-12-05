<h1 align="center">
<img src="images/cloud-connector-icon.png" />

OutSystems Cloud Connector
</h2>

![MIT][s0]

[s0]: https://img.shields.io/badge/license-MIT-blue.svg

## FAQs

### How do I run `outsystemscc` on Azure Container Instances?

The command to create a new container with the [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli) for the [Usage section example](README.md#usage) is:

    az container create \
      -g [ResourceGroupName] \
      --name [ContainerName] \
      --image ghcr.io/outsystems/outsystemscc \
      --command-line '/app/outsystemscc --header "token: N2YwMDIxZTEtNGUzNS1jNzgzLTRkYjAtYjE2YzRkZGVmNjcy" https://organization.outsystems.app/sg_f5696918-3a8c-4da8-8079-ef768d5479fd R:8081:192.168.0.3:8393'

The key parameters used in the command:

* `-g [ResourceGroupName]`: Specifies the name of the resource group where the container instance will be created.
* `--name [ContainerName]`: Specifies the name of the container instance.
* `--image ghcr.io/outsystems/outsystemscc`: Specifies the Docker image to use for the container instance.
* `--command-line '...'`: Specifies the command line to run in the container. This command starts the `outsystemscc` service with the specified header token, server URL, and remote connection details.

Ensure to replace `[ResourceGroupName]`, `[ContainerName]`, and the values in the `--command-line` parameter with your actual values.

#### Network configuration

* **Outbound Access to Internet:** Ensure that the Azure Resource Group in which you are deploying `outsystemscc` has outbound access to the Internet with no greater restriction than specified in [Firewall setup](#firewall-setup). This is crucial for `outsystemscc` to communicate with your ODC organization. You may need to configure your Network Security Groups (NSGs), Azure Firewall, or whichever network security solution you have in place to allow outbound connections.

* **Access to Endpoints:** Additionally, ensure that the network configuration allows traffic from the Azure Container Instance to the internal endpoint(s) you wish to connect to. This may involve configuring your Virtual Network (VNet), Subnets, and Network Security Groups (NSGs) to allow the necessary traffic. If there are firewalls or other network devices blocking traffic, you'll need to configure them accordingly.

* **Testing Connectivity:** It's a good practice to test the network connectivity before deploying `outsystemscc`. You can use tools like [Azure Network Watcher](https://docs.microsoft.com/en-us/azure/network-watcher/network-watcher-monitoring-overview) or even basic network troubleshooting tools like ping and traceroute to verify connectivity.

* **Monitoring and Logging:** Implement monitoring and logging to get insights into the network traffic and to troubleshoot any connectivity issues. Azure provides various monitoring and logging tools like [Azure Monitor](https://docs.microsoft.com/en-us/azure/azure-monitor/overview) and [Azure Log Analytics](https://docs.microsoft.com/en-us/azure/azure-monitor/log-query/log-analytics-tutorial) which can be invaluable for diagnosing network-related issues.

### **[⏎ Back to README](./README.md)**