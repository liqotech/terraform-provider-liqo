# Liqo provider

    Provider for Terraform to perform Liqo operations.

## Getting Started
Follow this example steps to test locally the implemented provider.

### Prerequisites
- [Terraform](https://developer.hashicorp.com/terraform/downloads)
- [Liqo CLI tool](https://docs.liqo.io/en/v0.6.1/installation/liqoctl.html)
- [go](https://go.dev/doc/install)

### Installation
1. in ***.terraform.d*** folder (you should have it in home/\<usr\>/) make directory with this command replacing _architecture_ with your architecture (example: linux_arm64 or linux_amd64):

    ``` mkdir -p /plugins/liqo-provider/liqo/liqo/0.0.1/<architecture>/ ```

    my complete path is the following:
        ```home/<usr>/.terraform.d/plugins/liqo-provider/liqo/liqo/0.0.1/linux_arm64/```

2. from root run command replacing _path_ with the one created in first step:

    ```go build -o <path>/terraform-provider-liqo ```

3. in your main.tf tell to Terraform to use provider implemented locally by yoursel with this directive in required_providers:

    ```source  = "liqo-provider/liqo/liqo"```

    for example:
    ```hcl
    terraform {
        required_providers {
            liqo = {
            source = "liqo-provider/liqo/liqo"
            }
        }
    }
    ```

4. run command:

    ```terraform init ```

    ```terraform apply -auto-approve```
