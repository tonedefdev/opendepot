opendepot {
  targets = ["copilot"]
}

agent_skill "changelog" {
  source  = "registry.example.com/acme/changelog"
  version = "0.3.2"
}

mcp_server "files" {
  source  = "registry.example.com/acme/files"
  version = "0.9.0"
}

mcp_server "github" {
  source  = "registry.example.com/acme/github"
  version = "1.0.0"
}

agent "planner" {
  source  = "registry.example.com/acme/release-planner"
  version = "1.2.0"
}

agent_skill "release-notes" {
  source  = "registry.example.com/acme/release-notes"
  version = "1.1.0"
}

agent "reviewer" {
  source  = "registry.example.com/acme/code-reviewer"
  version = "2.0.1"
}

agent "scanner" {
  source  = "registry.example.com/acme/dependency-scanner"
  version = "0.4.0"
}

agent "writer" {
  source  = "registry.example.com/acme/release-writer"
  version = "1.0.0"
}
