package parser

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"
)

// Disable directive grammar.
//
//   # segspec:disable=full
//   # segspec:disable=ingress
//   # segspec:disable=egress
//
// Equivalent k8s metadata form:
//
//   metadata:
//     labels:
//       segspec.io/disable: "full"      # or annotations: same key
//
// Driver: kubernetes/kubernetes #112560 — "Sometimes, I want to disable the
// networkpolicy temporarily. Now, I must delete it or edit it to match
// none." K8s upstream rejected this as a spec change; segspec fulfils it at
// the source-of-truth layer instead.

const (
	// DisableIngress skips ingress rule emission for the workload.
	DisableIngress = "ingress"
	// DisableEgress skips egress rule emission for the workload.
	DisableEgress = "egress"
	// DisableFull skips both ingress and egress (and per-service netpol
	// emission entirely in PerServiceNetworkPolicy).
	DisableFull = "full"
)

// disableInline matches `segspec:disable=<value>` anywhere inside a string.
// The leading `#` (yaml/properties/.env comment marker) is optional so the
// helper works on raw strings too — handy for unit tests and for mining
// values out of label/annotation text where the prefix is absent.
var disableInline = regexp.MustCompile(`segspec:disable\s*=\s*(ingress|egress|full)\b`)

// disableLabel matches the k8s label/annotation form
// `segspec.io/disable: "ingress"` (quotes optional).
var disableLabel = regexp.MustCompile(`segspec\.io/disable\s*[:=]\s*"?(ingress|egress|full)"?\b`)

// validDisableValues is the closed set of accepted directive values.
var validDisableValues = map[string]bool{
	DisableIngress: true,
	DisableEgress:  true,
	DisableFull:    true,
}

// ParseDisableDirective extracts a segspec disable directive from a single
// line of text. It accepts both the comment form (`# segspec:disable=full`)
// and the k8s label form (`segspec.io/disable: "full"`). Returns "" when
// no directive is found OR when the value is outside the accepted set —
// callers should treat unknown values as no-op rather than failing the
// parse, so a typo in a comment never breaks `segspec analyze`.
func ParseDisableDirective(line string) string {
	if line == "" {
		return ""
	}
	if m := disableInline.FindStringSubmatch(line); len(m) == 2 {
		if validDisableValues[m[1]] {
			return m[1]
		}
	}
	if m := disableLabel.FindStringSubmatch(line); len(m) == 2 {
		if validDisableValues[m[1]] {
			return m[1]
		}
	}
	return ""
}

// ScanFileDisable returns the first disable directive that appears anywhere
// in the file's comment/header region. It is used by parsers whose
// "workload" is the whole file (Spring application.yml, .env, build files
// pinned to a single artifact). Compose and k8s parsers do their own
// per-block scanning because a single file can host multiple workloads.
//
// Implementation notes:
//   - We deliberately scan the entire byte stream rather than only the
//     leading comments. A user putting `# segspec:disable=full` halfway down
//     a small properties file expects it to take effect; surprising them
//     with "the directive must be on line 1" would violate the principle of
//     least astonishment for a tool whose UX is "just write a comment".
//   - First match wins. Multiple conflicting directives are an authoring
//     smell; we don't escalate it to an error because the cheap-and-quiet
//     path matches the rest of segspec's parsing posture (skip what we
//     can't make sense of, never fail the whole run).
func ScanFileDisable(data []byte) string {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if d := ParseDisableDirective(scanner.Text()); d != "" {
			return d
		}
	}
	return ""
}

// ScanComposeDisable walks a docker-compose YAML byte stream and returns a
// map of service-name → directive for every `# segspec:disable=...` comment
// that immediately precedes a service block under `services:`. It is
// intentionally line-based rather than YAML-AST-based because comments are
// not preserved by `yaml.Unmarshal` — the AST round-trip drops them.
//
// Algorithm:
//
//  1. Find the line that opens the `services:` block (zero indent).
//  2. After that line, walk indented lines. The service-name lines are the
//     ones at depth-1 (the next indent level after `services:`) ending in
//     `:` and *not* starting with `-`.
//  3. The directive that "belongs to" a service is the most recent
//     unconsumed `# segspec:disable=...` comment encountered before the
//     service-name line. Once consumed, the buffered directive resets so
//     it does not bleed into the next service.
//
// This shape mirrors how operators actually write the comment in practice
// (right above the service they want to disable).
func ScanComposeDisable(data []byte) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	inServices := false
	servicesIndent := -1
	serviceIndent := -1
	pending := ""

	for scanner.Scan() {
		raw := scanner.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		// Detect entry to / exit from the `services:` block by indent.
		indent := leadingSpaces(raw)

		if !inServices {
			if indent == 0 && strings.HasPrefix(trimmed, "services:") {
				inServices = true
				servicesIndent = 0
			}
			continue
		}

		// A line back at services-indent (or less) that isn't a comment
		// terminates the services block.
		if indent <= servicesIndent && !strings.HasPrefix(trimmed, "#") {
			inServices = false
			pending = ""
			continue
		}

		// Buffer disable comments — they apply to the next service block.
		if strings.HasPrefix(trimmed, "#") {
			if d := ParseDisableDirective(trimmed); d != "" {
				pending = d
			}
			continue
		}

		// Detect the indent level of service entries on first encounter.
		if serviceIndent == -1 {
			serviceIndent = indent
		}

		// Service-name line: depth-1, ends with `:`, no `-` prefix.
		if indent == serviceIndent && strings.HasSuffix(trimmed, ":") && !strings.HasPrefix(trimmed, "-") {
			name := strings.TrimSuffix(trimmed, ":")
			name = strings.TrimSpace(name)
			if pending != "" && name != "" {
				result[name] = pending
			}
			pending = ""
		}
	}
	return result
}

// leadingSpaces counts the indent depth of a line in spaces. Tabs are
// treated as one space — Compose YAML in the wild is whitespace-only and
// tab-indented YAML is invalid per spec, so this is a safe simplification.
func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}

