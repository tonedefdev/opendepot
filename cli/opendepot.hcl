opendepot {
  targets = ["copilot", "claude"]
}

agent "code-review" {
  source = "opendepot.localtest.me:8443/opendepot-system/code-review"
  version = "~> 0.9.0"
}