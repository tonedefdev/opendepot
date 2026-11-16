package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

// presignAgentArchive returns a pre-signed URL for the stored archive of an agent Version. It returns an
// empty URL and a nil error when presign is not enabled, or when presign fails and FallbackToProxy is set,
// so the caller streams the bytes through the server. It returns an error when presign fails and fallback
// is not set. The caller must not redirect on error.
func presignAgentArchive(ctx context.Context, versionResource *opendepotv1alpha1.Version) (string, error) {
	agentSourceRef := versionResource.Spec.AgentSourceRef
	if agentSourceRef == nil || agentSourceRef.StorageConfig == nil || agentSourceRef.Name == nil || versionResource.Spec.FileName == nil {
		return "", nil
	}

	presignCfg := agentSourceRef.StorageConfig.Presign
	if presignCfg == nil || presignCfg.Enabled == nil || !*presignCfg.Enabled {
		return "", nil
	}

	presignedURL, err := generateAgentPresignedURL(ctx, versionResource, agentSourceRef.StorageConfig, presignCfg)
	if err == nil {
		return presignedURL, nil
	}

	if presignCfg.FallbackToProxy == nil || !*presignCfg.FallbackToProxy {
		return "", err
	}

	logger.Info("agent presign failed, falling back to proxy", "error", err, "version", versionResource.Name)

	return "", nil
}

func generateAgentPresignedURL(ctx context.Context, versionResource *opendepotv1alpha1.Version, storageConfig *opendepotv1alpha1.StorageConfig, presignCfg *opendepotv1alpha1.PresignConfig) (string, error) {
	if versionResource.Status.Checksum == nil {
		return "", fmt.Errorf("agent archive checksum unavailable for version '%s'", versionResource.Name)
	}

	name := strings.ToLower(versionResource.Spec.Type) + "-" + *versionResource.Spec.AgentSourceRef.Name
	objectKey := fmt.Sprintf("%s/%s", name, *versionResource.Spec.FileName)

	ttl := 15 * time.Minute
	if presignCfg.TTL != nil {
		ttl = presignCfg.TTL.Duration
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath:      &objectKey,
		ContainerName: &name,
		StorageConfig: storageConfig,
		PresignTTL:    ttl,
	}

	storageBackend, err := initStorageBackend(ctx, storageConfig)
	if err != nil {
		return "", fmt.Errorf("failed to init storage backend for agent presign: %w", err)
	}

	err = storageBackend.GetObjectChecksum(ctx, soi)
	if err != nil {
		return "", fmt.Errorf("failed to verify agent archive checksum before presign: %w", err)
	}

	if soi.ObjectChecksum == nil || *soi.ObjectChecksum != *versionResource.Status.Checksum {
		return "", errors.New("agent archive checksum mismatch; refusing to issue pre-signed URL")
	}

	err = storageBackend.PresignObject(ctx, soi)
	if err != nil {
		return "", fmt.Errorf("failed to presign agent archive: %w", err)
	}

	if soi.PresignedURL == nil {
		return "", errors.New("storage backend returned no pre-signed URL for agent archive")
	}

	return *soi.PresignedURL, nil
}
