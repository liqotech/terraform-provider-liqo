# 
resource "liqo_offload" "offload" {

  namespace                  = "liqo-demo"
  pod_offloading_strategy    = "LocalAndRemote"
  namespace_mapping_strategy = "DefaultName"
  cluster_selector_terms = [
    {
      match_expressions = [
        {
          key      = "region"
          operator = "In"
          values   = ["europe", "us-west"]
        },
      ]
    }
  ]

}
