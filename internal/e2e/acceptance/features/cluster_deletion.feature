Feature: Cluster deletion
  To keep teardown under the existing workflow
  As the soperator team
  We verify terraform destroy removes the e2e cluster

  Scenario: The workflow destroy step removes the cluster
    Then the workflow destroy step removes the e2e cluster
