---
# generated by https://github.com/hashicorp/terraform-plugin-docs
page_title: "liqo_generate Resource - liqo"
subcategory: ""
description: |-
  It retrieves the information concerning the local
  cluster (i.e., authentication endpoint and token, cluster ID, ...) to use
  on a different cluster to establish an out-of-band outgoing
  peering towards the local cluster.
---

# liqo_generate (Resource)

It retrieves the information concerning the local
cluster (i.e., authentication endpoint and token, cluster ID, ...) to use
on a different cluster to establish an out-of-band outgoing
peering towards the local cluster.



<!-- schema generated by tfplugindocs -->
## Schema

### Optional

- `liqo_namespace` (String) Namespace where Liqo is installed in provider cluster.

### Read-Only

- `auth_ep` (String) Provider authentication endpoint.
- `cluster_id` (String) Provider cluster ID.
- `cluster_name` (String) Provider cluster name.
- `local_token` (String) Provider authentication token.


