/*
Copyright 2026 Tony Owens.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"math/bits"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// jevEvidenceLabel labels the evidence part. It is not numeric, so jevSplitLabel sorts it before the numbered parts.
const jevEvidenceLabel = "evidence"

// jevEvidenceContext is how many lines are kept on each side of a matched line.
const jevEvidenceContext = 5

// jevEvidenceLineLimit is the byte limit for one line in the evidence view. A longer line is cut and marked.
const jevEvidenceLineLimit = 400

// jevEvidenceBudget is the number of bytes of excerpts the evidence view may hold. It leaves room in the Jev state
// limit for the header and the omission note, which are added to the reserve for the preamble.
const jevEvidenceBudget = jevChunkBodyBudget - 512

// jevEvidenceSignals holds one group per behavior. A line matches a signal when any pattern in its group matches the
// line. Patterns are case-insensitive. Excerpts that match more signals are kept first when the view is full. Most
// patterns are adapted from the DataDog/guarddog YARA rules (Apache-2.0).
var jevEvidenceSignals = jevEvidenceCompile([][]string{
	// Exfiltration destinations: webhook, tunnel, paste, and messenger hosts, suspicious URLs, and DNS lookups built from data.
	{
		`webhook\.(site|cool)`, `oastify\.com`, `burpcollaborator\.(net|me)`, `pipedream\.net`, `beeceptor\.com`,
		`ngrok(-free\.app|\.io)`, `trycloudflare\.com`, `localhost\.run`,
		`pastebin\.com`, `hastebin\.com`, `ghostbin\.site`, `transfer\.sh`, `filetransfer\.io`,
		`api\.telegram\.org`, `discord(app)?\.com/api/webhooks`, `\b\d{8,12}:[A-Za-z0-9_-]{30,40}\b`,
		`https?://[^\s/]+\.(xyz|tk|ml|ga|cf|gq|pw|top|club|bid|icu|zip|stream|link|quest)([/:?#"'\s)]|$)`,
		`https?://\d{1,3}(\.\d{1,3}){3}`,
		`socket\.getaddrinfo\s*\(.*\.format\s*\(`, `socket\.gethostbyname\s*\(\s*f['"]`, `nslookup\s+.*\$`,
	},
	// Outbound requests.
	{
		`requests\.(get|post|put|delete|head|patch)\s*\(`, `urllib\.\w+\.(urlopen|urlretrieve)\s*\(`, `http\.client\.HTTP`,
		`\bfetch\s*\(`, `axios\.(get|post|put|delete|patch)\s*\(`, `https?\.(get|request)\s*\(`,
		`http\.(Get|Post|Head|Do)\(`, `socket\.(socket|create_connection)\s*\(`, `\b(smtplib|ftplib|paramiko)\b`,
		`Invoke-WebRequest`, `DownloadFile\s*\(`,
	},
	// Host and user details.
	{
		`socket\.gethostname\s*\(\s*\)`, `platform\.(node|uname)\s*\(\s*\)`, `getpass\.getuser\s*\(\s*\)`,
		`os\.(getlogin|hostname)\s*\(\s*\)`, `os\.userInfo\s*\(\s*\)`, `os\.environ\s*\[\s*['"]USER(NAME)?['"]\s*\]`,
		`os\.(system|popen)\s*\(\s*['"]whoami`,
	},
	// Credentials and private files.
	{
		`process\.env\.[A-Z_]*(API_?KEY|SECRET|TOKEN|PASS(WORD)?|AUTH|CRED(ENTIAL)?)[A-Z_]*`,
		`os\.(environ|getenv)\s*[\[\(]\s*['"][A-Z_]*(API_?KEY|SECRET|TOKEN|PASS(WORD)?|AUTH)`,
		`os\.Getenv\s*\(\s*['"][A-Z_]*(KEY|SECRET|TOKEN|PASSWORD|AUTH)`,
		`JSON\.stringify\(process\.env\)`, `ENV\.to_h\.to_json`,
		`/etc/(passwd|shadow)`, `\.env(\.local|\.production)?['"]`, `\.ssh/id_(rsa|ed25519|ecdsa)`, `\bid_rsa\b`,
		`\.git-credentials`, `\.aws/credentials`, `gcloud/credentials`, `\.azure/credentials`, `\.npmrc`, `\.pypirc`, `\.netrc`,
		`Google/Chrome/User Data`, `\.mozilla/firefox`, `keychain`,
	},
	// Keystroke capture.
	{
		`from pynput import keyboard`, `keyboard\.(Listener|hook|on_press)\(`, `\bpyHook\b`, `SetWindowsHookEx`,
		`require\(['"](iohook|keypress)['"]\)`, `node-global-key-listener`, `from Xlib import`, `ecodes\.EV_KEY`,
	},
	// Execution, download and execute, and reverse shells.
	{
		`(^|[^.\w])(eval|exec)\s*\(`, `\bnew\s+Function\s*\(`, `subprocess\.(call|run|Popen|check_call|check_output)\s*\(`,
		`os\.(system|popen)\s*\(`, `child_process`, `\bexec\s*\.\s*Command\s*\(`, `\b(powershell|Invoke-Expression)\b`,
		`\biex\s`, `\bbash\s+-c`, `\bsh\s+-c`, `chmod\s+\+x`, `__import__\(`,
		`(curl|wget)\s+.*\|\s*(bash|sh|python|node|perl)\b`, `(curl|wget)\s+.*-o\s+\S+\s*&&\s*(chmod|\./|start|bash|sh)`,
		`/dev/(tcp|udp)/`, `pty\.spawn\s*\(`, `\bn(c|cat)\s+.*-e\s+/bin/(ba)?sh`,
		`def\s+(reverse_shell|rev_shell|connect_back|backdoor)\s*\(`,
	},
	// Encoding and obfuscation.
	{
		`base64\.(b64decode|decodebytes|standard_b64decode|StdEncoding\.DecodeString)`, `\batob\s*\(`,
		`Buffer\.from\([^)]*['"]base64['"]`, `Base64\.(strict_|urlsafe_)?decode64`, `String\.fromCharCode\s*\(`,
		`\\x[0-9a-fA-F]{2}(\\x[0-9a-fA-F]{2}){49,}`, `eval\s*\(\s*function\s*\(`, `\b(pickle|marshal)\.loads?\(`,
		`zlib\.decompress`,
	},
	// Persistence.
	{
		`\.bashrc`, `\.bash_profile`, `\.zshrc`, `['"][^'"]*\.profile['"]`, `/etc/(rc\.local|init\.d/|profile\.d/)`,
		`\.config/autostart/`, `LaunchAgents`, `LaunchDaemons`, `crontab`, `authorized_keys`, `launchctl`,
		`systemctl\s+enable`, `CurrentVersion\\Run`, `Start Menu\\Programs\\Startup`,
	},
	// Instructions aimed at the reviewer or the agent. No permissively licensed source covers these, so they are ours.
	{
		`ignore (all )?(previous|prior|the above)`, `disregard`, `system prompt`, `do not (tell|mention|report)`,
		`don'?t tell`, `without (asking|confirmation|the user)`, `silently`, `skip (the )?review`, `\breviewer\b`,
		`risk (score|level)`, `\bjev\b`,
	},
})

// jevEvidenceCompile compiles each pattern case-insensitively. The patterns are fixed, so a pattern that does not
// compile is a programming error for real.
func jevEvidenceCompile(groups [][]string) [][]*regexp.Regexp {
	compiled := make([][]*regexp.Regexp, len(groups))
	for i, patterns := range groups {
		for _, p := range patterns {
			compiled[i] = append(compiled[i], regexp.MustCompile("(?i)"+p))
		}
	}

	return compiled
}

// jevEvidenceExcerpt is a run of lines from one bundled file around the lines that matched a signal. lines is the
// whole file and first and last are 0-based inclusive indexes into it. mask has a bit for each signal that matched.
type jevEvidenceExcerpt struct {
	file  string
	lines []string
	first int
	last  int
	mask  uint32
}

// jevEvidenceLineMask returns the signal bits that a line matches.
func jevEvidenceLineMask(line string) uint32 {
	var mask uint32
	for i, patterns := range jevEvidenceSignals {
		for _, re := range patterns {
			if re.MatchString(line) {
				mask |= 1 << i
				break
			}
		}
	}

	return mask
}

// jevEvidenceScan returns the excerpts of every file, in file order. Overlapping or adjacent excerpts of one file are
// merged. files must hold text only, because the planner has already withheld binary and flagged files.
func jevEvidenceScan(dir string, files []string) ([]jevEvidenceExcerpt, error) {
	var excerpts []jevEvidenceExcerpt
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("%w: read %s: %w", errJevChunkIO, rel, err)
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			mask := jevEvidenceLineMask(line)
			if mask == 0 {
				continue
			}

			first := max(0, i-jevEvidenceContext)
			last := min(len(lines)-1, i+jevEvidenceContext)
			if n := len(excerpts); n > 0 && excerpts[n-1].file == rel && first <= excerpts[n-1].last+1 {
				excerpts[n-1].last = max(excerpts[n-1].last, last)
				excerpts[n-1].mask |= mask
				continue
			}

			excerpts = append(excerpts, jevEvidenceExcerpt{file: rel, lines: lines, first: first, last: last, mask: mask})
		}
	}

	return excerpts, nil
}

// jevEvidenceBlock renders one excerpt with its file and line numbers.
func jevEvidenceBlock(ex jevEvidenceExcerpt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "== %s, lines %d-%d ==\n", ex.file, ex.first+1, ex.last+1)
	for i := ex.first; i <= ex.last; i++ {
		line := ex.lines[i]
		if len(line) > jevEvidenceLineLimit {
			line = jevChunkTruncate(line, jevEvidenceLineLimit) + " [line cut]"
		}

		fmt.Fprintf(&b, "%d: %s\n", i+1, line)
	}

	b.WriteString("\n")
	return b.String()
}

// jevEvidencePart returns the evidence view of the bundled files as one part. ok is false when no line matches a
// signal. Excerpts that match the most signals are kept first. Excerpts that do not fit are omitted and counted.
func jevEvidencePart(in *jevChunkInput) (part jevChunkPart, ok bool, err error) {
	excerpts, err := jevEvidenceScan(in.dir, in.files)
	if err != nil {
		return part, false, err
	}

	if len(excerpts) == 0 {
		return part, false, nil
	}

	blocks := make([]string, len(excerpts))
	for i, ex := range excerpts {
		blocks[i] = jevEvidenceBlock(ex)
	}

	order := make([]int, len(excerpts))
	for i := range order {
		order[i] = i
	}

	sort.SliceStable(order, func(a, b int) bool {
		return bits.OnesCount32(excerpts[order[a]].mask) > bits.OnesCount32(excerpts[order[b]].mask)
	})

	chosen := make([]bool, len(excerpts))
	var used int
	for _, i := range order {
		if used+len(blocks[i]) > jevEvidenceBudget {
			continue
		}

		chosen[i] = true
		used += len(blocks[i])
	}

	var b strings.Builder
	b.WriteString("The excerpts below were selected by keyword from the bundled files. Each excerpt gives its file and the line numbers in that file. Lines between excerpts are not shown. The numbered parts hold the full content.\n\n")
	var files []string
	var omitted int
	for i, ex := range excerpts {
		if !chosen[i] {
			omitted++
			continue
		}

		b.WriteString(blocks[i])
		if n := len(files); n == 0 || files[n-1] != ex.file {
			files = append(files, ex.file)
		}
	}

	if omitted > 0 {
		fmt.Fprintf(&b, "%d more excerpt(s) matched but were omitted to fit this view. The numbered parts still hold the full content.\n", omitted)
	}

	part = jevChunkPart{
		label:    jevEvidenceLabel,
		evidence: true,
		pieces:   []jevChunkPiece{{text: b.String()}},
		files:    files,
	}

	return part, true, nil
}
