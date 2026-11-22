# Merges Go coverage profiles into one profile per go.work module.
#
# Usage:
#   awk -v mods="pkg/storage services/depot" -v outdir=DIR -f hack/merge-coverage.awk profile...
#
# Every block is keyed by its position (file:start.col,end.col numStmt). Hit counts for
# the same key are summed across all inputs, so each block appears once per module.
# Each file is assigned to the module with the longest matching directory prefix, so a
# file never lands in two output profiles. Mixed modes are allowed (set, count, atomic);
# the output uses the strongest mode among the inputs.

BEGIN {
	prefix = "github.com/tonedefdev/opendepot/"
	nmods = split(mods, list, " ")
	for (i = 1; i <= nmods; i++) {
		modset[list[i]] = 1
	}
}

$1 == "mode:" {
	rank = ($2 == "set") ? 0 : ($2 == "count") ? 1 : ($2 == "atomic") ? 2 : -1
	if (rank < 0) {
		printf "merge-coverage: unknown mode: %s (%s)\n", $2, FILENAME > "/dev/stderr"
		exit 1
	}

	if (mode == "" || rank > maxrank) {
		maxrank = rank
		mode = $2
	}

	next
}

NF >= 3 {
	count = $NF
	key = $0
	sub(/ [^ ]+$/, "", key)

	file = key
	sub(/:.*/, "", file)
	if (index(file, prefix) != 1) {
		unmatched++
		next
	}

	rel = substr(file, length(prefix) + 1)
	best = ""
	for (m in modset) {
		if (index(rel, m "/") == 1 && length(m) > length(best)) {
			best = m
		}
	}

	if (best == "") {
		unmatched++
		next
	}

	if (!((best, key) in hits)) {
		order[best] = order[best] "\n" key
	}

	hits[best, key] += count
}

END {
	if (mode == "") {
		print "merge-coverage: no mode line found in inputs" > "/dev/stderr"
		exit 1
	}

	if (unmatched > 0) {
		printf "merge-coverage: skipped %d blocks outside go.work modules\n", unmatched > "/dev/stderr"
	}

	for (m in order) {
		name = m
		gsub(/\//, "-", name)
		out = outdir "/" name ".out"
		print "mode: " mode > out

		n = split(order[m], keys, "\n")
		for (i = 1; i <= n; i++) {
			if (keys[i] != "") {
				print keys[i] " " hits[m, keys[i]] > out
			}
		}

		close(out)
	}
}
