group "default" {
  targets = [
    "resource-broker",
  ]
}

target "resource-broker" {
  context = "."
  dockerfile = "Dockerfile"
  tags = [
    "ghcr.io/platform-mesh/resource-broker:dev",
  ]
  platforms = [
    "linux/amd64",
    "linux/arm64",
  ]
}
