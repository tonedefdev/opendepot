package ocibuild

// Literals shared by the ocibuild tests. Repeated values live here so that goconst has one definition to point at.
const (
	testRoot = "acme-modules-0123456789abcdef0123456789abcdef01234567"
	testApp  = "app"

	testLinux         = "linux"
	testAmd64         = "amd64"
	testArm           = "arm"
	testArm64         = "arm64"
	testPlatformAmd64 = "linux/amd64"
	testPlatformArm64 = "linux/arm64"
	testPlatformArmV7 = "linux/arm/v7"

	testNamespace     = "opendepot-builds"
	testSource        = "https://github.com/acme/modules"
	testBrokerURL     = "http://version-controller-build-broker.opendepot.svc:8090"
	testBuildKitImage = "moby/buildkit:v0.35.1-rootless@sha256:" +
		"7d02292556e66feea6d2d89b7ec143503b57e4f59a4efc6b7ece8547e4d3e5be"
	testHelperImage = "opendepot/version-controller@sha256:" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testBuildID     = "3f2a9c1d-0e4b-4a5f-8c7d-9e0f1a2b3c4d"
	testFetchToken  = "fetch-token-value"
	testUploadToken = "upload-token-value"
)

const testDockerfile = "FROM alpine:3.20\nCOPY app/ /app/\n"
