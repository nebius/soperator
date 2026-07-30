Feature: Observability stack
  Scenario: kube-state-metrics scrape config is consumed by the vm-stack chart
    Then the kube-state-metrics VMServiceScrape carries the soperator scrape endpoints
