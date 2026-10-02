package main

import "strings"

func splitResourceKey(s string) (ns, kind, name string, ok bool) {
	parts := strings.SplitN(s, "/", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}

	return parts[0], parts[1], parts[2], true
}

func splitVersionKey(s string) (ns, kind, name, version string, ok bool) {
	parts := strings.SplitN(s, "/", 4)
	if len(parts) != 4 {
		return "", "", "", "", false
	}

	return parts[0], parts[1], parts[2], parts[3], true
}
