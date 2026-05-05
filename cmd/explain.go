package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/explainer"
	"github.com/dormstern/segspec/internal/parser/netpol"
	"github.com/dormstern/segspec/internal/renderer"
)

// explainPoliciesPath, explainLabels, explainNamespace, explainJSON are
// flag-bound state for `segspec explain`. Reset by tests via t.Cleanup,
// matching the pattern used by validate / demo.
var (
	explainPoliciesPath string
	explainLabels       string
	explainNamespace    string
	explainJSON         bool
)

var explainCmd = &cobra.Command{
	Use:   "explain <workload-name>",
	Short: "Explain which NetworkPolicies apply to a workload and what they allow",
	Long: `explain takes a workload name and a directory of NetworkPolicy
manifests, finds every policy that selects the workload (via its labels),
and prints the union of allow-rules — the workload's effective allow-set —
with file:line evidence per contributed rule.

Kubernetes NetworkPolicy is additive: multiple policies that select the
same workload union their rules, and the presence of any policy flips the
workload from allow-by-default to deny-by-default for the affected
direction. This command makes that non-local behavior visible.

Examples:
  segspec explain api --policies ./policies/ --labels app=api
  segspec explain api --policies ./policies/ --labels app=api,tier=web --namespace prod
  segspec explain api --policies ./policies/ --labels app=api --json

Free tier — no license required.`,
	Args: cobra.ExactArgs(1),
	RunE: runExplain,
}

func init() {
	explainCmd.Flags().StringVar(&explainPoliciesPath, "policies", "", "Path to a NetworkPolicy file or directory (required)")
	explainCmd.Flags().StringVar(&explainLabels, "labels", "", "Comma-separated key=value workload labels (e.g. app=api,tier=web)")
	explainCmd.Flags().StringVar(&explainNamespace, "namespace", "", "Workload namespace (used to scope policy matching)")
	explainCmd.Flags().BoolVar(&explainJSON, "json", false, "Emit structured JSON instead of Markdown")
	rootCmd.AddCommand(explainCmd)
}

func runExplain(cmd *cobra.Command, args []string) error {
	name := args[0]
	if explainPoliciesPath == "" {
		return fmt.Errorf("--policies is required (path to a NetworkPolicy file or directory)")
	}

	pr, err := netpol.ReadPath(explainPoliciesPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", explainPoliciesPath, err)
	}

	labels, err := parseLabelString(explainLabels)
	if err != nil {
		return err
	}

	w := explainer.Workload{
		Name:      name,
		Namespace: explainNamespace,
		Labels:    labels,
	}

	exp := explainer.Explain(w, pr.Policies)

	out := cmd.OutOrStdout()

	if explainJSON {
		body, err := renderer.ExplainJSON(exp)
		if err != nil {
			return fmt.Errorf("render json: %w", err)
		}
		fmt.Fprintln(out, string(body))
		return nil
	}

	fmt.Fprint(out, renderer.ExplainMarkdown(exp))
	return nil
}

// parseLabelString accepts "k1=v1,k2=v2" form and returns the map. Empty
// input is allowed and means "no labels" — useful for explaining what
// would apply to a fully-unlabeled pod (typically: nothing).
func parseLabelString(s string) (map[string]string, error) {
	out := map[string]string{}
	s = strings.TrimSpace(s)
	if s == "" {
		return out, nil
	}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		idx := strings.Index(pair, "=")
		if idx <= 0 || idx == len(pair)-1 {
			return nil, fmt.Errorf("invalid label %q: expected key=value", pair)
		}
		key := strings.TrimSpace(pair[:idx])
		val := strings.TrimSpace(pair[idx+1:])
		out[key] = val
	}
	return out, nil
}
