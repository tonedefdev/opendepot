package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/go-chi/chi/v5"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
	"github.com/tonedefdev/opendepot/pkg/storage"
	storageTypes "github.com/tonedefdev/opendepot/pkg/storage/types"
)

// sanitizeModuleVersionForLookup normalizes a module version string into the form
// used as the Version resource name suffix: strips a leading "v", lowercases, and
// replaces dots and underscores with hyphens (e.g. "v1.2.3" → "1-2-3").
func sanitizeModuleVersionForLookup(version string) string {
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	version = strings.ToLower(version)
	version = strings.ReplaceAll(version, ".", "-")
	version = strings.ReplaceAll(version, "_", "-")
	return version
}

// getModuleVersion fetches the Version resource for the module name and version
// encoded in the request URL parameters.
func getModuleVersion(clientset *kubernetes.Clientset, w http.ResponseWriter, r *http.Request) (*opendepotv1alpha1.Version, error) {
	name := chi.URLParam(r, "name")
	namespace := chi.URLParam(r, "namespace")
	version := chi.URLParam(r, "version")
	moduleName := fmt.Sprintf("%s-%s", name, sanitizeModuleVersionForLookup(version))

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("versions").
		Name(moduleName).
		DoRaw(r.Context())
	if err != nil {
		return nil, err
	}

	var moduleVersion opendepotv1alpha1.Version
	if err = json.Unmarshal(result, &moduleVersion); err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return nil, err
	}

	return &moduleVersion, nil
}

// getDownloadModuleUrl handles the Terraform module download redirect endpoint.
// It resolves the caller's identity, enforces GroupBinding access control, looks up
// the Version resource, and responds with an X-Terraform-Get header that directs
// the Terraform client to the storage-backend-specific download URL.
func getDownloadModuleUrl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, _, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	if binding != nil {
		name := chi.URLParam(r, "name")
		namespace := chi.URLParam(r, "namespace")

		if !isResourceAllowed(binding, "module", name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
	}

	moduleVersion, err := getModuleVersion(clientset, w, r)
	if err != nil {
		logger.Error("unable to get module version", "error", err)
		return
	}

	if moduleVersion.Spec.ModuleConfigRef == nil ||
		moduleVersion.Spec.ModuleConfigRef.StorageConfig == nil ||
		moduleVersion.Spec.FileName == nil {
		http.Error(w, "module version artifact not yet available", http.StatusServiceUnavailable)
		return
	}

	if moduleVersion.Status.Checksum == nil {
		http.Error(w, "module version checksum not yet available", http.StatusServiceUnavailable)
		return
	}
	downloadPath, err := buildDownloadPathFromVersion(moduleVersion)
	if err != nil {
		logger.Error("unable to build module download path", "error", err, "version", moduleVersion.Name)
		http.Error(w, "module version artifact not available", http.StatusServiceUnavailable)

		return
	}

	wrapper, err := resolveModuleArchiveWrapper(r.Context(), moduleVersion)
	if err != nil {
		logger.Error("unable to resolve module archive root", "error", err, "version", moduleVersion.Name)
		http.Error(w, "module version artifact not available", http.StatusServiceUnavailable)

		return
	}

	query := url.Values{}
	query.Set("archive", moduleArchiveType(*moduleVersion.Spec.FileName))
	query.Set("fileChecksum", *moduleVersion.Status.Checksum)
	w.Header().Set("X-Terraform-Get", "/opendepot/modules/v1/download/"+downloadPath+"//"+url.PathEscape(wrapper)+"?"+query.Encode())
	// Download is recorded here (at the protocol redirect step) rather than in the
	// serveModuleFrom* handlers because the Terraform module protocol requires clients
	// to call this endpoint first; namespace and version are only available here.
	// The serveModuleFrom* routes do not carry namespace or version URL params.
	err = recordDownload(r.Context(), chi.URLParam(r, "namespace"), "module", chi.URLParam(r, "name"), chi.URLParam(r, "version"))
	if err != nil {
		logger.Error("failed to record download", "error", err, "namespace", chi.URLParam(r, "namespace"), "module", chi.URLParam(r, "name"), "version", chi.URLParam(r, "version"))
	}

	w.WriteHeader(http.StatusNoContent)
}

// getModuleVersions returns the list of available versions for a module, as required
// by the Terraform module registry protocol.
func getModuleVersions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	clientset, binding, _, subject, err := getKubeClientFromRequest(w, r)
	if err != nil {
		logger.Error("unable to generate kubeclient", "error", err)
		return
	}

	namespace := chi.URLParam(r, "namespace")
	name := chi.URLParam(r, "name")

	if binding != nil {
		if !isResourceAllowed(binding, "module", name) {
			logger.Warn("resource access denied", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		logger.Info("resource access allowed", "subject", subject, "binding_name", binding.Name, "resource_type", "module", "resource_name", name, "namespace", namespace)
	}

	result, err := clientset.RESTClient().
		Get().
		AbsPath("/apis/opendepot.defdev.io/v1alpha1").
		Namespace(namespace).
		Resource("modules").
		Name(name).
		DoRaw(r.Context())
	if err != nil {
		logger.Error("unable to get modules", "error", err, "namespace", namespace, "name", name, "responseBody", string(result))

		switch {
		case k8sApiErrors.IsNotFound(err):
			http.Error(w, "module not found", http.StatusNotFound)
		case k8sApiErrors.IsForbidden(err):
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}

		return
	}

	var module opendepotv1alpha1.Module
	if err = json.Unmarshal(result, &module); err != nil {
		logger.Error("unable to unmarshal module", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	response := ModuleVersionsResponse{
		Modules: []ModuleVersions{
			{
				Versions: module.Spec.Versions,
			},
		},
	}

	json.NewEncoder(w).Encode(response)
}

// serveModuleFromAzureBlob proxies a module archive download from Azure Blob Storage.
func serveModuleFromAzureBlob(w http.ResponseWriter, r *http.Request) {
	accountName := chi.URLParam(r, "account")
	accountUrl := chi.URLParam(r, "accountUrl")
	rg := chi.URLParam(r, "rg")
	subID := chi.URLParam(r, "subID")

	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	accountUrl, err := url.PathUnescape(accountUrl)
	if err != nil {
		logger.Error("failed to unescape account url", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	azureStorage := &storage.AzureBlobStorage{}
	if err := azureStorage.NewClients(subID, accountUrl); err != nil {
		logger.Error("failed to init azure clients", "error", err, "storageAccountName", accountName)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath:      &fileName,
		Method:        storageTypes.Get,
		ContainerName: &name,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			AzureStorage: &opendepotv1alpha1.AzureStorageConfig{
				AccountName:    accountName,
				AccountUrl:     accountUrl,
				ResourceGroup:  rg,
				SubscriptionID: subID,
			},
		},
	}
	getObjectFromStorageSystem(w, r, azureStorage, soi, checksum)
}

// serveModuleFromFileSystem serves a module archive from the local filesystem.
func serveModuleFromFileSystem(w http.ResponseWriter, r *http.Request) {
	encodedDir := chi.URLParam(r, "directory")
	moduleName := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	dirBytes, err := base64.RawURLEncoding.DecodeString(encodedDir)
	if err != nil {
		logger.Error("failed to decode directory path", "error", err)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}
	dir := string(dirBytes)

	filePath := path.Join(dir, moduleName, fileName)
	allowedRoot := path.Clean(*opendepotFilesystemMountPath)
	if !strings.HasPrefix(path.Clean(filePath)+"/", allowedRoot+"/") {
		logger.Error("filesystem download path escapes allowed root", "path", filePath, "allowedRoot", allowedRoot)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	logger.Info("filesystem download", "dir", dir, "module", moduleName, "file", fileName)

	fsStorage := &storage.FileSystem{}
	soi := &storageTypes.StorageObjectInput{
		FilePath: &filePath,
		Method:   storageTypes.Get,
	}
	getObjectFromStorageSystem(w, r, fsStorage, soi, checksum)
}

func moduleArchiveType(fileName string) string {
	if strings.HasSuffix(fileName, ".tar.gz") {
		return "tar.gz"
	}

	archiveType := strings.TrimPrefix(path.Ext(fileName), ".")
	if archiveType == "tar" {
		return "tar.gz"
	}

	return archiveType
}

func moduleArchiveWrapper(reader io.Reader, archiveType string) (string, error) {
	var entryName string
	switch archiveType {
	case "zip":
		buffered := bufio.NewReader(reader)
		header := make([]byte, 30)
		if _, err := io.ReadFull(buffered, header); err != nil {
			return "", fmt.Errorf("read ZIP header: %w", err)
		}
		if binary.LittleEndian.Uint32(header[:4]) != 0x04034b50 {
			return "", fmt.Errorf("invalid ZIP local file header")
		}
		nameLength := int(binary.LittleEndian.Uint16(header[26:28]))
		name := make([]byte, nameLength)
		if _, err := io.ReadFull(buffered, name); err != nil {
			return "", fmt.Errorf("read ZIP entry name: %w", err)
		}
		entryName = string(name)
	case "tar.gz":
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			return "", fmt.Errorf("open gzip archive: %w", err)
		}
		defer gzipReader.Close()

		header, err := tar.NewReader(gzipReader).Next()
		if err != nil {
			return "", fmt.Errorf("read tar header: %w", err)
		}
		entryName = header.Name
	default:
		return "", fmt.Errorf("unsupported module archive type %q", archiveType)
	}

	cleanEntry := strings.TrimPrefix(path.Clean(entryName), "./")
	wrapper := strings.SplitN(cleanEntry, "/", 2)
	if wrapper[0] == "" || wrapper[0] == "." || wrapper[0] == ".." || (len(wrapper) != 2 && !strings.HasSuffix(entryName, "/")) {
		return "", fmt.Errorf("module archive does not contain a single wrapper directory")
	}

	return wrapper[0], nil
}

// serveModuleFromGCS proxies a module archive download from Google Cloud Storage.
func serveModuleFromGCS(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	gcsStorage := &storage.GoogleCloudStorage{}
	if err := gcsStorage.NewClient(r.Context()); err != nil {
		logger.Error("failed to init gcs client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			GCS: &opendepotv1alpha1.GoogleCloudStorageConfig{
				Bucket: bucket,
			},
		},
	}
	getObjectFromStorageSystem(w, r, gcsStorage, soi, checksum)
}

// serveModuleFromS3 proxies a module archive download from Amazon S3.
func serveModuleFromS3(w http.ResponseWriter, r *http.Request) {
	bucket := chi.URLParam(r, "bucket")
	region := chi.URLParam(r, "region")
	name := chi.URLParam(r, "name")
	fileName := chi.URLParam(r, "fileName")
	checksum := r.URL.Query().Get("fileChecksum")

	s3Storage := &storage.AmazonS3Storage{}
	if err := s3Storage.NewClient(r.Context(), region); err != nil {
		logger.Error("failed to init s3 client", "error", err, "bucket", bucket)
		http.Error(w, "failed to get module", http.StatusInternalServerError)
		return
	}

	soi := &storageTypes.StorageObjectInput{
		FilePath: aws.String(fmt.Sprintf("%s/%s", name, fileName)),
		Method:   storageTypes.Get,
		StorageConfig: &opendepotv1alpha1.StorageConfig{
			S3: &opendepotv1alpha1.AmazonS3Config{
				Bucket: bucket,
			},
		},
	}
	getObjectFromStorageSystem(w, r, s3Storage, soi, checksum)
}
