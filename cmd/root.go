package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dormstern/segspec/internal/license"
)

var (
	outputFormat string
	outputFile   string
	licenseKey   string

	// activeClaims holds the verified license claims for the current
	// invocation, or nil if no license was supplied. It is populated in
	// PersistentPreRunE and read by gating logic in analyze.go and diff.go.
	activeClaims *license.Claims
)

// Exit codes used by segspec. Kept centralized so commands can return errors
// and Execute() maps them to the right os.Exit code.
const (
	exitOK            = 0
	exitGenericError  = 1
	exitLicenseDenied = 2
)

// errLicenseRequired marks a paid-feature gate failure. Execute() maps these
// to exitLicenseDenied (2) so CI scripts can distinguish "license problem"
// from "analysis problem".
type errLicenseRequired struct{ msg string }

func (e *errLicenseRequired) Error() string { return e.msg }

func newLicenseError(format string, args ...any) error {
	return &errLicenseRequired{msg: fmt.Sprintf(format, args...)}
}

var rootCmd = &cobra.Command{
	Use:   "segspec",
	Short: "Generate Kubernetes NetworkPolicy from application configs",
	Long: `segspec analyzes application configuration files and generates
Kubernetes NetworkPolicy YAML for microsegmentation.

Point it at your app directory. It reads configs, infers network
dependencies, and outputs ready-to-apply policies.

  segspec analyze ./my-app/
  segspec analyze ./my-app/ --format netpol
  segspec analyze ./my-app/ --ai`,
	SilenceErrors:     true, // we render errors ourselves in Execute
	SilenceUsage:      true,
	PersistentPreRunE: resolveLicense,
}

// resolveLicense reads --license-key (or SEGSPEC_LICENSE_KEY) and validates
// it against the in-binary public key. It is intentionally lenient: a missing
// key is fine (the user is on the free tier), but an invalid or expired key
// is a hard failure even if the requested feature is free — that's a clearer
// signal than silently downgrading.
func resolveLicense(cmd *cobra.Command, args []string) error {
	raw := strings.TrimSpace(licenseKey)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("SEGSPEC_LICENSE_KEY"))
	}
	if raw == "" {
		activeClaims = nil
		return nil
	}

	claims, err := license.Validate(raw, license.ProductionPublicKey)
	if err != nil {
		var expErr *license.ExpiredError
		if errors.As(err, &expErr) {
			return newLicenseError("License expired %s. Renew at https://segspec.dev/pro",
				expErr.ExpiredAt.UTC().Format("2006-01-02"))
		}
		return newLicenseError("Invalid license key: %v", err)
	}
	activeClaims = claims
	return nil
}

func Execute() {
	err := rootCmd.Execute()
	if err == nil {
		return
	}

	// errChangesDetected is the diff command's "found changes" signal. It's
	// not a user-facing error — the diff body has already been printed — so
	// we exit silently with code 1.
	if errors.Is(err, errChangesDetected) {
		os.Exit(exitGenericError)
	}

	// Everything else gets rendered to stderr.
	fmt.Fprintln(os.Stderr, "Error: "+err.Error())

	var licenseErr *errLicenseRequired
	if errors.As(err, &licenseErr) {
		os.Exit(exitLicenseDenied)
	}
	os.Exit(exitGenericError)
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "format", "f", "summary", "Output format: summary, netpol, per-service, default-deny, all, evidence, audit, json")
	rootCmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "", "Write output to file (default: stdout)")
	rootCmd.PersistentFlags().StringVar(&licenseKey, "license-key", "", "Pro/Enterprise license key (or set SEGSPEC_LICENSE_KEY). Get one at https://segspec.dev/pro")
}
