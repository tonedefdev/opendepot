package ocibuild

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// DefaultMaxCompressedBytes is the largest repository archive, as downloaded, that StageContext accepts.
const DefaultMaxCompressedBytes int64 = 256 << 20

// DefaultMaxDecompressedBytes is the largest decompressed tar stream that StageContext and Unpack accept.
const DefaultMaxDecompressedBytes int64 = 1 << 30

// DefaultMaxEntries is the largest number of tar entries that StageContext and Unpack accept.
const DefaultMaxEntries = 100000

const (
	contextDirName    = "context"
	dockerfileDirName = "dockerfile"
	dockerfileName    = "Dockerfile"
	dockerignoreName  = dockerfileName + ".dockerignore"
	packedDirMode     = 0o755
	packedFileMode    = 0o644
	packedExecMode    = 0o755
)

var (
	// ErrLimitExceeded is returned when an archive or a file is larger than the configured limit.
	ErrLimitExceeded = errors.New("limit exceeded")

	// ErrUnsafeEntry is returned when an archive entry could escape the extraction root or touch the host.
	ErrUnsafeEntry = errors.New("unsafe archive entry")
)

// Limits bounds how much an archive may cost to read. A zero field takes its default.
type Limits struct {
	// MaxCompressedBytes bounds the archive as it is read from the source.
	MaxCompressedBytes int64
	// MaxDecompressedBytes bounds the tar stream after gzip decompression.
	MaxDecompressedBytes int64
	// MaxEntries bounds the number of tar headers.
	MaxEntries int
}

// DefaultLimits returns the limits for a build context: 256 MiB compressed, 1 GiB decompressed, and 100k entries.
func DefaultLimits() Limits {
	return Limits{
		MaxCompressedBytes:   DefaultMaxCompressedBytes,
		MaxDecompressedBytes: DefaultMaxDecompressedBytes,
		MaxEntries:           DefaultMaxEntries,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxCompressedBytes <= 0 {
		l.MaxCompressedBytes = d.MaxCompressedBytes
	}

	if l.MaxDecompressedBytes <= 0 {
		l.MaxDecompressedBytes = d.MaxDecompressedBytes
	}

	if l.MaxEntries <= 0 {
		l.MaxEntries = d.MaxEntries
	}

	return l
}

// ContextSpec selects the build context and Dockerfile inside a repository archive. Both paths are relative to the
// repository root. An empty ContextPath means the root, and an empty DockerfilePath means DefaultDockerfilePath.
type ContextSpec struct {
	ContextPath    string
	DockerfilePath string
}

// StageContext reads a GitHub repository archive from src, validates it, and writes a deterministic tar.gz to dst.
// The archive holds context/** (the build context), dockerfile/Dockerfile, and dockerfile/Dockerfile.dockerignore when
// the repository has one next to the Dockerfile. The top-level directory of the archive is stripped. workDir must be
// an empty directory that the caller removes afterward. It holds the extracted tree.
//
// Symlinks that resolve outside the repository root are rejected. Symlinks in the context that resolve outside the
// context directory are rejected too, so the packed context never points at files it does not contain. Hardlinks,
// devices, FIFOs, and absolute or '..' paths are rejected. Submodules and Git LFS objects are not included, because
// GitHub archives omit them.
func StageContext(dst io.Writer, src io.Reader, workDir string, spec ContextSpec, limits Limits) error {
	contextPath, err := ResolveContextPath(spec.ContextPath)
	if err != nil {
		return err
	}

	dockerfilePath, err := ResolveDockerfilePath(spec.DockerfilePath)
	if err != nil {
		return err
	}

	if err := extract(src, workDir, true, limits.withDefaults()); err != nil {
		return err
	}

	if err := requireDir(workDir, contextPath); err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	dockerfile, found, err := readRegularFile(workDir, dockerfilePath)
	if err != nil {
		return fmt.Errorf("dockerfile: %w", err)
	}

	if !found {
		return fmt.Errorf("dockerfile %q: %w", dockerfilePath, fs.ErrNotExist)
	}

	if err := CheckDockerfile(dockerfile); err != nil {
		return err
	}

	dockerignore, _, err := readRegularFile(workDir, dockerfilePath+".dockerignore")
	if err != nil {
		return fmt.Errorf("dockerignore: %w", err)
	}

	return pack(dst, workDir, packPlan{
		contextPath:  contextPath,
		dockerfile:   dockerfile,
		dockerignore: dockerignore,
	})
}

// Unpack extracts a tar.gz from src into dst, which must exist and be empty. It applies the same validation as
// StageContext, except that it keeps the archive's paths as they are. The fetch helper uses it to restore the
// output of StageContext into the workspace.
func Unpack(src io.Reader, dst string, limits Limits) error {
	return extract(src, dst, false, limits.withDefaults())
}

// packPlan holds the Dockerfile content that StageContext reads before it packs the tree.
type packPlan struct {
	contextPath  string
	dockerfile   []byte
	dockerignore []byte
}

// extract writes every entry of the tar.gz in src beneath dst. When stripRoot is true the first path segment of every
// entry must be the same, and it is removed.
func extract(src io.Reader, dst string, stripRoot bool, limits Limits) error {
	gz, err := gzip.NewReader(limitReader(src, limits.MaxCompressedBytes))
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}

	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(limitReader(gz, limits.MaxDecompressedBytes))
	x := &extractor{dst: dst, stripRoot: stripRoot, symlinks: map[string]struct{}{}}
	entries := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}

		entries++
		if entries > limits.MaxEntries {
			return fmt.Errorf("%w: more than %d entries", ErrLimitExceeded, limits.MaxEntries)
		}

		// GitHub archives begin with a pax_global_header record. It carries no files.
		if hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}

		if err := x.entry(hdr, tr); err != nil {
			return err
		}
	}
}

// extractor tracks the symlinks it has created, so that no later entry is written through one. Lexical checks alone
// do not stop a path such as link/evil from landing outside dst once link points elsewhere.
type extractor struct {
	dst       string
	stripRoot bool
	root      string
	haveRoot  bool
	symlinks  map[string]struct{}
}

func (x *extractor) entry(hdr *tar.Header, body io.Reader) error {
	rel, err := archiveRelPath(hdr.Name)
	if err != nil {
		return err
	}

	if x.stripRoot {
		rel, err = x.stripTopLevel(rel)
		if err != nil {
			return err
		}
	}

	if rel == "" || rel == "." {
		if hdr.Typeflag != tar.TypeDir {
			return fmt.Errorf("%w: %q must be a directory", ErrUnsafeEntry, hdr.Name)
		}

		return nil
	}

	if err := x.checkNoSymlinkOnPath(rel); err != nil {
		return err
	}

	full := filepath.Join(x.dst, filepath.FromSlash(rel))
	switch hdr.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(full, packedDirMode)
	case tar.TypeReg:
		return x.writeFile(full, hdr, body)
	case tar.TypeSymlink:
		return x.writeSymlink(rel, full, hdr.Linkname)
	default:
		return fmt.Errorf("%w: %q has unsupported type %q", ErrUnsafeEntry, hdr.Name, hdr.Typeflag)
	}
}

// stripTopLevel removes the shared top-level directory from rel. It returns an empty string for that directory.
func (x *extractor) stripTopLevel(rel string) (string, error) {
	first, rest, _ := strings.Cut(rel, "/")
	if !x.haveRoot {
		x.root, x.haveRoot = first, true
	}

	if first != x.root {
		return "", fmt.Errorf("%w: %q is outside the top-level directory %q", ErrUnsafeEntry, rel, x.root)
	}

	return rest, nil
}

func (x *extractor) checkNoSymlinkOnPath(rel string) error {
	parts := strings.Split(rel, "/")
	for i := range parts {
		if _, ok := x.symlinks[strings.Join(parts[:i+1], "/")]; ok {
			return fmt.Errorf("%w: %q is written through a symlink", ErrUnsafeEntry, rel)
		}
	}

	return nil
}

func (x *extractor) writeFile(full string, hdr *tar.Header, body io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(full), packedDirMode); err != nil {
		return fmt.Errorf("create parent of %q: %w", hdr.Name, err)
	}

	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, packedFileModeFor(hdr.Mode))
	if err != nil {
		return fmt.Errorf("create %q: %w", hdr.Name, err)
	}

	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %q: %w", hdr.Name, err)
	}

	return f.Close()
}

func (x *extractor) writeSymlink(rel, full, target string) error {
	if !symlinkStaysInRoot(rel, target) {
		return fmt.Errorf("%w: symlink %q -> %q resolves outside the repository", ErrUnsafeEntry, rel, target)
	}

	if err := os.MkdirAll(filepath.Dir(full), packedDirMode); err != nil {
		return fmt.Errorf("create parent of %q: %w", rel, err)
	}

	if err := os.Symlink(target, full); err != nil {
		return fmt.Errorf("create symlink %q: %w", rel, err)
	}

	x.symlinks[rel] = struct{}{}
	return nil
}

// archiveRelPath validates an archive entry name and returns it cleaned and relative. It rejects absolute names,
// backslashes, NUL bytes, and '..' segments. An empty result or "." is the root.
func archiveRelPath(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return "", fmt.Errorf("%w: entry name %q", ErrUnsafeEntry, name)
	}

	for segment := range strings.SplitSeq(strings.TrimSuffix(name, "/"), "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: entry name %q climbs out of the archive", ErrUnsafeEntry, name)
		}
	}

	return path.Clean(name), nil
}

// symlinkStaysInRoot reports whether target, resolved relative to the directory that holds the symlink at rel, stays
// inside the repository root. Absolute targets and targets that climb above the root are rejected.
func symlinkStaysInRoot(rel, target string) bool {
	if target == "" || strings.HasPrefix(target, "/") || strings.ContainsAny(target, "\\\x00") {
		return false
	}

	return within(".", path.Join(path.Dir(rel), target))
}

// within reports whether p is dir or lies beneath it. A dir of "." contains every relative path that does not climb
// above the root.
func within(dir, p string) bool {
	if dir == "." {
		return p != ".." && !strings.HasPrefix(p, "../")
	}

	return p == dir || strings.HasPrefix(p, dir+"/")
}

func packedFileModeFor(mode int64) fs.FileMode {
	if mode&0o111 != 0 {
		return packedExecMode
	}

	return packedFileMode
}

// pack writes the deterministic tar.gz. Entries are taken from a sorted walk of root, so the output does not depend
// on the order of the source archive. It stores no timestamps, owners, or names of users and groups.
func pack(dst io.Writer, root string, plan packPlan) (err error) {
	gz := gzip.NewWriter(dst)
	tw := tar.NewWriter(gz)
	defer func() {
		if cerr := tw.Close(); err == nil {
			err = cerr
		}

		if cerr := gz.Close(); err == nil {
			err = cerr
		}
	}()

	if err := writeDirHeader(tw, contextDirName); err != nil {
		return err
	}

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}

		rel = filepath.ToSlash(rel)
		if rel == "." || rel == plan.contextPath || !within(plan.contextPath, rel) {
			return nil
		}

		inner := rel
		if plan.contextPath != "." {
			inner = strings.TrimPrefix(rel, plan.contextPath+"/")
		}

		return packEntry(tw, p, d, contextDirName+"/"+inner, func(target string) bool {
			return within(plan.contextPath, path.Join(path.Dir(rel), target))
		})
	})
	if walkErr != nil {
		return walkErr
	}

	if err := writeDirHeader(tw, dockerfileDirName); err != nil {
		return err
	}

	if err := writeBytes(tw, dockerfileDirName+"/"+dockerfileName, plan.dockerfile); err != nil {
		return err
	}

	if plan.dockerignore != nil {
		return writeBytes(tw, dockerfileDirName+"/"+dockerignoreName, plan.dockerignore)
	}

	return nil
}

// packEntry writes one walked entry. insideContext reports whether a symlink target stays in the context.
func packEntry(tw *tar.Writer, p string, d fs.DirEntry, name string, insideContext func(target string) bool) error {
	info, err := d.Info()
	if err != nil {
		return err
	}

	switch {
	case d.IsDir():
		return writeDirHeader(tw, name)
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(p)
		if err != nil {
			return err
		}

		if !insideContext(target) {
			return fmt.Errorf("%w: symlink %q -> %q leaves the build context", ErrUnsafeEntry, name, target)
		}

		return writeHeaderOnly(tw, &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777})
	case info.Mode().IsRegular():
		return writeFileFrom(tw, name, p, info.Mode().Perm())
	default:
		return fmt.Errorf("%w: %q is not a regular file, directory, or symlink", ErrUnsafeEntry, name)
	}
}

func writeFileFrom(tw *tar.Writer, name, p string, perm fs.FileMode) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: info.Size(), Mode: int64(packedFileModeFor(int64(perm)))}
	if err := writeHeaderOnly(tw, hdr); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}

func writeBytes(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(data)), Mode: packedFileMode}
	if err := writeHeaderOnly(tw, hdr); err != nil {
		return err
	}

	_, err := tw.Write(data)
	return err
}

func writeDirHeader(tw *tar.Writer, name string) error {
	return writeHeaderOnly(tw, &tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: packedDirMode})
}

// writeHeaderOnly normalizes a header to the deterministic form and writes it. Timestamps, owners, and names are
// fixed, so the same content always produces the same bytes.
func writeHeaderOnly(tw *tar.Writer, hdr *tar.Header) error {
	hdr.ModTime = time.Unix(0, 0)
	hdr.AccessTime = time.Time{}
	hdr.ChangeTime = time.Time{}
	hdr.Uid, hdr.Gid = 0, 0
	hdr.Uname, hdr.Gname = "", ""
	return tw.WriteHeader(hdr)
}

// lstatInTree returns the Lstat of rel beneath root. It fails when an intermediate component is not a real
// directory, so a symlink on the path is never followed.
func lstatInTree(root, rel string) (fs.FileInfo, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}

	if rel == "." {
		return info, nil
	}

	current := root
	for segment := range strings.SplitSeq(rel, "/") {
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: %q is not a directory", ErrUnsafeEntry, current)
		}

		current = filepath.Join(current, segment)
		info, err = os.Lstat(current)
		if err != nil {
			return nil, err
		}
	}

	return info, nil
}

func requireDir(root, rel string) error {
	info, err := lstatInTree(root, rel)
	if err != nil {
		return fmt.Errorf("%q: %w", rel, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a directory", ErrUnsafeEntry, rel)
	}

	return nil
}

// readRegularFile returns the content of the regular file at rel beneath root. found is false when the file does not
// exist. A symlink, a directory, or a file over MaxDockerfileBytes is an error.
func readRegularFile(root, rel string) (data []byte, found bool, err error) {
	info, err := lstatInTree(root, rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, err
	}

	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%w: %q is not a regular file", ErrUnsafeEntry, rel)
	}

	if info.Size() > MaxDockerfileBytes {
		return nil, false, fmt.Errorf("%w: %q is %d bytes, more than %d",
			ErrLimitExceeded, rel, info.Size(), MaxDockerfileBytes)
	}

	data, err = os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return nil, false, err
	}

	return data, true, nil
}

// limitedReader reads at most a fixed number of bytes. It returns ErrLimitExceeded when the source has more.
type limitedReader struct {
	r         io.Reader
	remaining int64
}

func limitReader(r io.Reader, n int64) io.Reader {
	return &limitedReader{r: r, remaining: n}
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		var probe [1]byte
		n, err := l.r.Read(probe[:])
		if n > 0 {
			return 0, ErrLimitExceeded
		}

		return 0, err
	}

	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}

	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	return n, err
}
