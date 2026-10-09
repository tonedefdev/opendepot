agent "opendepot.localtest.me:8443/opendepot-system/code-review" {
  version        = "0.9.0"
  constraints    = "~> 0.9.0"
  signing_key    = "F293651BA42FE9E0C32C8283E17CC7D19FF4697D"
  signature_hash = "sha256:5251d0dbbf5128243498395419ad3fb289281ec7debbf8b507399a4470e0e1d7"
  hashes         = ["h1:ayYmyeS9n4Y3pCrcupBjjtsF4OqYPVYMK0eVsfeMesE=", "zh:195884315e59f75663d4311f2cda8aed52c7c0836bd5ee63467f4cf1e351b1c7"]

  installed "claude" {
    path = ".claude/agents/code-review.md"
    hash = "h1:x+F85gdmFoYTPX04EttEo2ZBXq/yoLl4/6O9Ea+p7tE="
  }

  installed "copilot" {
    path = ".github/agents/code-review.agent.md"
    hash = "h1:6q6J/03pD6LmvR8/3H7XeR2C4sQb1jAS0GBwDivSmIk="
  }
}
