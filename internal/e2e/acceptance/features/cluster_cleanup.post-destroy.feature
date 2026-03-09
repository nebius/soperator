Feature: Cluster cleanup after acceptance flow
  To keep the existing workflow ownership of teardown
  As the soperator team
  We verify cluster deletion after terraform destroy finishes

  Scenario: Terraform destroy removes the e2e cluster
    Then the workflow destroy step removes the e2e cluster
