opendepot {
  targets = ["copilot"]
}

agent "context7" {
  source = "opendepot.localtest.me:8443/opendepot-system/context7"
  version = "1.0.0"
}

agent_skill "acquire-codebase-knowledge" {
  source = "opendepot.localtest.me:8443/opendepot-system/acquire-codebase-knowledge"
  version = "~> 1.0.0"
}

agent_skill "appinsights-instrumentation" {
  source = "opendepot.localtest.me:8443/opendepot-system/appinsights-instrumentation"
  version = "1.0.0"
}