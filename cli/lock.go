package main

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

const lockFileName = "opendepot.lock.hcl"

type lockFile struct {
	Skills []lockEntry `hcl:"agent_skill,block"`
	Agents []lockEntry `hcl:"agent,block"`
}

type lockEntry struct {
	Source      string          `hcl:"source,label"`
	Version     string          `hcl:"version"`
	Constraints string          `hcl:"constraints"`
	SigningKey  string          `hcl:"signing_key"`
	Signature   string          `hcl:"signature_hash"`
	Hashes      []string        `hcl:"hashes"`
	Installed   []lockInstalled `hcl:"installed,block"`
}

type lockInstalled struct {
	Target string `hcl:"target,label"`
	Path   string `hcl:"path"`
	Hash   string `hcl:"hash"`
}

// find returns the lock entry for a source address.
func (l *lockFile) find(source string) (lockEntry, bool) {
	for _, list := range [][]lockEntry{l.Skills, l.Agents} {
		for _, e := range list {
			if e.Source == source {
				return e, true
			}
		}
	}

	return lockEntry{}, false
}

// hash returns the lock hash with the given prefix ("h1:" or "zh:").
func (e lockEntry) hash(prefix string) string {
	for _, h := range e.Hashes {
		if len(h) > len(prefix) && h[:len(prefix)] == prefix {
			return h
		}
	}

	return ""
}

// loadLock reads the lock file. A missing file yields an empty lock.
func loadLock(path string) (*lockFile, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return &lockFile{}, nil
	}

	parser := hclparse.NewParser()
	file, diags := parser.ParseHCLFile(path)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse %s: %s", path, diags.Error())
	}

	var lock lockFile
	diags = gohcl.DecodeBody(file.Body, nil, &lock)
	if diags.HasErrors() {
		return nil, fmt.Errorf("decode %s: %s", path, diags.Error())
	}

	return &lock, nil
}

// renderLock returns the canonical HCL encoding of the lock file. Entries are sorted by source
// and installed targets by label, so rendering is stable across runs.
func renderLock(lock *lockFile) []byte {
	f := hclwrite.NewEmptyFile()
	body := f.Body()
	first := true
	for _, group := range []struct {
		block   string
		entries []lockEntry
	}{{"agent_skill", lock.Skills}, {"agent", lock.Agents}} {
		entries := append([]lockEntry(nil), group.entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Source < entries[j].Source })
		for _, e := range entries {
			if !first {
				body.AppendNewline()
			}

			first = false
			appendLockBlock(body, group.block, e)
		}
	}

	return hclwrite.Format(f.Bytes())
}

func appendLockBlock(body *hclwrite.Body, blockType string, e lockEntry) {
	block := body.AppendNewBlock(blockType, []string{e.Source})
	bb := block.Body()
	bb.SetAttributeValue("version", cty.StringVal(e.Version))
	bb.SetAttributeValue("constraints", cty.StringVal(e.Constraints))
	bb.SetAttributeValue("signing_key", cty.StringVal(e.SigningKey))
	bb.SetAttributeValue("signature_hash", cty.StringVal(e.Signature))
	bb.SetAttributeValue("hashes", stringList(e.Hashes))

	installed := append([]lockInstalled(nil), e.Installed...)
	sort.Slice(installed, func(i, j int) bool { return installed[i].Target < installed[j].Target })
	for _, in := range installed {
		bb.AppendNewline()
		ib := bb.AppendNewBlock("installed", []string{in.Target})
		ib.Body().SetAttributeValue("path", cty.StringVal(in.Path))
		ib.Body().SetAttributeValue("hash", cty.StringVal(in.Hash))
	}
}

func stringList(values []string) cty.Value {
	if len(values) == 0 {
		return cty.ListValEmpty(cty.String)
	}

	vals := make([]cty.Value, 0, len(values))
	for _, v := range values {
		vals = append(vals, cty.StringVal(v))
	}

	return cty.ListVal(vals)
}
