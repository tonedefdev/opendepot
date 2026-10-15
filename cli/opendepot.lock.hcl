agent_skill "opendepot.localtest.me:8443/opendepot-system/acquire-codebase-knowledge" {
  version        = "1.0.0"
  constraints    = "~> 1.0.0"
  signing_key    = "F293651BA42FE9E0C32C8283E17CC7D19FF4697D"
  signature_hash = "sha256:17c924be8f2f95a638cfd539a78060c374ba35ea58e220cc49f869debf947406"
  hashes         = ["h1:THQgwxu+vlYPt82Bfz3TDO016rwERo4WkYrbAuCnlng=", "zh:a8e5e75a927c6de1f9b28d82bf8e4c2cb2e0f06330413ed8bd40372f8ffde429"]

  installed "copilot" {
    path = ".github/skills/acquire-codebase-knowledge"
    hash = "h1:THQgwxu+vlYPt82Bfz3TDO016rwERo4WkYrbAuCnlng="
  }
}

agent_skill "opendepot.localtest.me:8443/opendepot-system/appinsights-instrumentation" {
  version        = "1.0.0"
  constraints    = "1.0.0"
  signing_key    = "F293651BA42FE9E0C32C8283E17CC7D19FF4697D"
  signature_hash = "sha256:33cafc291b0637cd56e79588ff3946a96fbb6fe97aeb82eb2dd0dc10fd440927"
  hashes         = ["h1:kjOkN6K0YJAk+dQC6Y0rZx8Gp9/5dgE7C64EyoKF5ds=", "zh:3fa2b7f1af0df2b5896f1d0906f9924249fe554d03e3d5fe5448ba808861aad5"]

  installed "copilot" {
    path = ".github/skills/appinsights-instrumentation"
    hash = "h1:kjOkN6K0YJAk+dQC6Y0rZx8Gp9/5dgE7C64EyoKF5ds="
  }
}

agent "opendepot.localtest.me:8443/opendepot-system/context7" {
  version        = "1.0.0"
  constraints    = "1.0.0"
  signing_key    = "F293651BA42FE9E0C32C8283E17CC7D19FF4697D"
  signature_hash = "sha256:b4c8d8afe41431edfae74a31ed329f4f7aa5e3bc4c5bfe493064a3673de2ce22"
  hashes         = ["h1:+B11ifjVs7B2R8jrYi1ENaVJOBWDiGcHyEDqXyxw9Ro=", "zh:1e173d8c14799d4974e8b7d4e669632f0566cba0e502312fa1609d04b20aa13c"]

  installed "copilot" {
    path = ".github/agents/context7.agent.md"
    hash = "h1:+B11ifjVs7B2R8jrYi1ENaVJOBWDiGcHyEDqXyxw9Ro="
  }
}
