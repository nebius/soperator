Feature: Observability stack
  @soperator_version_>=5.0.0
  Scenario: kube-state-metrics scrape config is consumed by the vm-stack chart
    Then the kube-state-metrics VMServiceScrape carries the soperator scrape endpoints
