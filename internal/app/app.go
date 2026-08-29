package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/codejavu-llc/ghemails/internal/cache"
	"github.com/codejavu-llc/ghemails/internal/extract"
	"github.com/codejavu-llc/ghemails/internal/githubapi"
	"github.com/codejavu-llc/ghemails/internal/model"
	"github.com/codejavu-llc/ghemails/internal/report"
	"github.com/codejavu-llc/ghemails/internal/scan"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

type options struct {
	domains           []string
	domainsFile       string
	mode              string
	visibility        string
	sources           []string
	repos             []string
	orgs              []string
	includeSubdomains bool
	includeNoreply    bool
	maxRepos          int
	maxCommits        int
	maxHistoryBytes   int64
	historyTimeout    time.Duration
	concurrency       int
	requestTimeout    time.Duration
	apiURL            string
	token             string
	tokenFile         string
	output            string
	format            string
	force             bool
	baseline          string
	requireComplete   bool
	failOnFindings    bool
	noCache           bool
	cacheDir          string
	cacheTTL          time.Duration
	quiet             bool
	verbose           bool
}

func Execute(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRoot(stdout, stderr)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	var exit *exitError
	if errors.As(err, &exit) {
		if exit.err != nil {
			fmt.Fprintf(stderr, "[!] %v\n", exit.err)
		}
		return exit.code
	}
	fmt.Fprintf(stderr, "[!] %v\n", err)
	return 1
}

func newRoot(stdout, stderr io.Writer) *cobra.Command {
	legacy := defaultOptions()
	root := &cobra.Command{
		Use:           "ghemails",
		Short:         "Evidence-first GitHub email reconnaissance",
		Long:          "ghemails discovers directly observed target-domain emails in GitHub code, discussions, metadata, and history. Use only for authorized security work.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if len(legacy.domains) == 0 && legacy.domainsFile == "" {
				return &exitError{code: 2, err: errors.New("a domain is required; use -d example.com or ghemails scan -d example.com")}
			}
			return runScans(cmd.Context(), legacy, stdout, stderr)
		},
	}
	addScanFlags(root, &legacy)

	scanOptions := defaultOptions()
	scanCommand := &cobra.Command{
		Use:   "scan",
		Short: "Scan one or more authorized domains",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScans(cmd.Context(), scanOptions, stdout, stderr)
		},
	}
	addScanFlags(scanCommand, &scanOptions)
	root.AddCommand(scanCommand)

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(stdout, "ghemails %s (commit %s, built %s)\n", Version, Commit, Date)
			return err
		},
	})

	completion := &cobra.Command{Use: "completion [bash|zsh|fish|powershell]", Short: "Generate shell completion", Args: cobra.ExactArgs(1)}
	completion.RunE = func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return root.GenBashCompletion(stdout)
		case "zsh":
			return root.GenZshCompletion(stdout)
		case "fish":
			return root.GenFishCompletion(stdout, true)
		case "powershell":
			return root.GenPowerShellCompletion(stdout)
		default:
			return &exitError{code: 2, err: fmt.Errorf("unsupported shell %q", args[0])}
		}
	}
	root.AddCommand(completion)

	cacheCommand := &cobra.Command{Use: "cache", Short: "Manage cached GitHub responses"}
	cacheDir := cache.DefaultDir()
	clearCommand := &cobra.Command{
		Use:   "clear",
		Short: "Delete cached GitHub responses",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if err := cache.Clear(cacheDir); err != nil {
				return err
			}
			_, err := fmt.Fprintf(stdout, "cleared %s\n", cacheDir)
			return err
		},
	}
	clearCommand.Flags().StringVar(&cacheDir, "cache-dir", cacheDir, "cache directory to clear")
	cacheCommand.AddCommand(clearCommand)
	root.AddCommand(cacheCommand)
	return root
}

func defaultOptions() options {
	return options{
		mode: "balanced", visibility: "public", maxRepos: 100, maxCommits: 1000,
		maxHistoryBytes: 100 << 20, historyTimeout: 5 * time.Minute, concurrency: 4,
		requestTimeout: 45 * time.Second, apiURL: "https://api.github.com", format: "txt",
		cacheDir: cache.DefaultDir(), cacheTTL: 24 * time.Hour,
	}
}

func addScanFlags(command *cobra.Command, value *options) {
	flags := command.Flags()
	flags.StringArrayVarP(&value.domains, "domain", "d", nil, "target domain (repeatable)")
	flags.StringVar(&value.domainsFile, "domains", "", "file containing one target domain per line")
	flags.StringVar(&value.mode, "mode", value.mode, "scan mode: fast, balanced, or deep")
	flags.StringVar(&value.visibility, "visibility", value.visibility, "GitHub visibility: public or accessible")
	flags.StringSliceVar(&value.sources, "sources", nil, "source override: repositories,code,issues,commits,repo-history,identity,git-history")
	flags.StringArrayVar(&value.repos, "repo", nil, "authorized owner/repository scope (repeatable)")
	flags.StringArrayVar(&value.orgs, "org", nil, "authorized GitHub organization scope (repeatable)")
	flags.BoolVar(&value.includeSubdomains, "include-subdomains", false, "include mail domains below the target domain")
	flags.BoolVar(&value.includeNoreply, "include-noreply", false, "include GitHub noreply addresses when they match")
	flags.IntVar(&value.maxRepos, "max-repos", value.maxRepos, "maximum candidate repositories")
	flags.IntVar(&value.maxCommits, "max-commits", value.maxCommits, "maximum API commits per candidate repository")
	flags.Int64Var(&value.maxHistoryBytes, "max-history-bytes", value.maxHistoryBytes, "maximum git-log bytes per deep repository")
	flags.DurationVar(&value.historyTimeout, "history-timeout", value.historyTimeout, "timeout per deep clone/history operation")
	flags.IntVarP(&value.concurrency, "concurrency", "c", value.concurrency, "bounded content worker count")
	flags.DurationVar(&value.requestTimeout, "timeout", value.requestTimeout, "HTTP request timeout")
	flags.StringVar(&value.apiURL, "api-url", value.apiURL, "GitHub REST API base URL")
	flags.StringVarP(&value.token, "token", "t", "", "deprecated: GitHub token; prefer GH_TOKEN/GITHUB_TOKEN or --token-file")
	_ = flags.MarkDeprecated("token", "use GH_TOKEN, GITHUB_TOKEN, or --token-file to avoid exposing secrets in process listings")
	flags.StringVar(&value.tokenFile, "token-file", "", "file containing one GitHub token")
	flags.StringVarP(&value.output, "output", "o", "", "report path (stdout when omitted)")
	flags.StringVarP(&value.format, "format", "f", value.format, "output: txt, jsonl, json, csv, markdown, or sarif")
	flags.BoolVar(&value.force, "force", false, "overwrite an existing report")
	flags.StringVar(&value.baseline, "baseline", "", "prior txt/JSON/JSONL report whose emails should be suppressed")
	flags.BoolVar(&value.requireComplete, "require-complete", false, "exit 3 if any source is partial")
	flags.BoolVar(&value.failOnFindings, "fail-on-findings", false, "exit 4 when new findings are emitted")
	flags.BoolVar(&value.noCache, "no-cache", false, "disable response cache")
	flags.StringVar(&value.cacheDir, "cache-dir", value.cacheDir, "response cache directory")
	flags.DurationVar(&value.cacheTTL, "cache-ttl", value.cacheTTL, "response cache lifetime")
	flags.BoolVarP(&value.quiet, "quiet", "q", false, "suppress progress and summaries")
	flags.BoolVarP(&value.verbose, "verbose", "v", false, "show rate-limit and retry details")
}

func runScans(ctx context.Context, opts options, stdout, stderr io.Writer) error {
	domains, err := loadDomains(opts)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	if len(domains) == 0 {
		return &exitError{code: 2, err: errors.New("at least one domain is required")}
	}
	if opts.requestTimeout <= 0 || opts.historyTimeout <= 0 || (!opts.noCache && opts.cacheTTL <= 0) {
		return &exitError{code: 2, err: errors.New("request, history, and enabled-cache timeouts must be positive")}
	}
	if _, ok := report.Formats[opts.format]; !ok {
		return &exitError{code: 2, err: fmt.Errorf("unsupported format %q", opts.format)}
	}
	if len(domains) > 1 && opts.format != "txt" && opts.format != "json" && opts.format != "jsonl" {
		return &exitError{code: 2, err: errors.New("multiple domains support txt, json, or jsonl output")}
	}
	token, err := resolveToken(opts)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	sources := opts.sources
	needsCode := len(sources) == 0
	for _, source := range sources {
		if strings.EqualFold(strings.TrimSpace(source), "code") {
			needsCode = true
		}
	}
	if needsCode && token == "" {
		return &exitError{code: 2, err: errors.New("GitHub code search requires authentication; set GH_TOKEN/GITHUB_TOKEN or --token-file")}
	}
	cached := &cache.Cache{Dir: opts.cacheDir, TTL: opts.cacheTTL, Enabled: !opts.noCache}
	var apiNotify func(string)
	if opts.verbose && !opts.quiet {
		apiNotify = func(message string) { fmt.Fprintf(stderr, "[*] %s\n", message) }
	}
	client, err := githubapi.NewClient(opts.apiURL, token, opts.requestTimeout, cached, apiNotify)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	var progress func(string)
	if !opts.quiet {
		progress = func(message string) { fmt.Fprintf(stderr, "[*] %s\n", message) }
	}
	runs := make([]model.Run, 0, len(domains))
	for _, domain := range domains {
		config := scan.Config{
			Domain: domain, Mode: opts.mode, Visibility: opts.visibility, Sources: append([]string(nil), opts.sources...), Repos: opts.repos, Orgs: opts.orgs,
			IncludeSubdomains: opts.includeSubdomains, IncludeNoreply: opts.includeNoreply, MaxRepos: opts.maxRepos, MaxCommits: opts.maxCommits,
			MaxHistoryBytes: opts.maxHistoryBytes, HistoryTimeout: opts.historyTimeout, Concurrency: opts.concurrency,
			GitToken: token,
		}
		scanner, err := scan.New(config, client, progress)
		if err != nil {
			return &exitError{code: 2, err: err}
		}
		run := scanner.Run(ctx)
		if opts.baseline != "" {
			known, err := report.LoadBaseline(opts.baseline)
			if err != nil {
				return &exitError{code: 2, err: fmt.Errorf("load baseline: %w", err)}
			}
			report.ApplyBaseline(&run, known)
		}
		runs = append(runs, run)
		if !opts.quiet {
			fmt.Fprintf(stderr, "[*] %s: %d email(s), run %s\n", domain, len(run.Findings), run.Status)
		}
	}
	writer, closeWriter, err := outputWriter(opts, stdout)
	if err != nil {
		return &exitError{code: 2, err: err}
	}
	if closeWriter != nil {
		defer func() {
			if closeWriter != nil {
				_ = closeWriter()
			}
		}()
	}
	if err := writeRuns(runs, opts.format, writer); err != nil {
		return &exitError{code: 1, err: err}
	}
	if closeWriter != nil {
		if err := closeWriter(); err != nil {
			return &exitError{code: 1, err: fmt.Errorf("close report: %w", err)}
		}
		closeWriter = nil
	}
	anyFailed, anyPartial, findings := false, false, 0
	for _, run := range runs {
		anyFailed = anyFailed || run.Status == "failed"
		anyPartial = anyPartial || run.Status == "partial"
		findings += len(run.Findings)
	}
	if anyFailed {
		return &exitError{code: 1, err: errors.New("all selected discovery sources failed for at least one target")}
	}
	if anyPartial && opts.requireComplete {
		return &exitError{code: 3, err: errors.New("run is partial and --require-complete was set")}
	}
	if findings > 0 && opts.failOnFindings {
		return &exitError{code: 4, err: fmt.Errorf("%d new email finding(s)", findings)}
	}
	return nil
}

func loadDomains(opts options) ([]string, error) {
	values := append([]string(nil), opts.domains...)
	if opts.domainsFile != "" {
		data, err := os.ReadFile(opts.domainsFile)
		if err != nil {
			return nil, fmt.Errorf("read domains file: %w", err)
		}
		values = append(values, strings.Fields(string(data))...)
	}
	seen := make(map[string]struct{})
	result := make([]string, 0, len(values))
	for _, value := range values {
		domain, err := extract.NormalizeDomain(value)
		if err != nil {
			return nil, fmt.Errorf("invalid domain %q: %w", value, err)
		}
		if _, duplicate := seen[domain]; !duplicate {
			seen[domain] = struct{}{}
			result = append(result, domain)
		}
	}
	sort.Strings(result)
	return result, nil
}

func resolveToken(opts options) (string, error) {
	if opts.tokenFile != "" {
		info, err := os.Stat(opts.tokenFile)
		if err != nil {
			return "", fmt.Errorf("stat token file: %w", err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			return "", fmt.Errorf("token file permissions are %o; require 0600 or stricter", info.Mode().Perm())
		}
		data, err := os.ReadFile(opts.tokenFile)
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		fields := strings.Fields(string(data))
		if len(fields) != 1 {
			return "", errors.New("token file must contain exactly one token; token rotation is not supported")
		}
		return fields[0], nil
	}
	if opts.token != "" {
		return strings.TrimSpace(opts.token), nil
	}
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return token, nil
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN")), nil
}

func outputWriter(opts options, stdout io.Writer) (io.Writer, func() error, error) {
	if opts.output == "" {
		return stdout, nil, nil
	}
	if info, err := os.Lstat(filepath.Clean(opts.output)); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("refusing to write report through symlink %s", opts.output)
	}
	flags := os.O_WRONLY | os.O_CREATE
	if opts.force {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}
	file, err := os.OpenFile(filepath.Clean(opts.output), flags, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, nil, fmt.Errorf("report %s already exists; use --force to replace it", opts.output)
		}
		return nil, nil, err
	}
	return file, file.Close, nil
}

func writeRuns(runs []model.Run, format string, writer io.Writer) error {
	if len(runs) == 1 {
		return report.Write(runs[0], format, writer)
	}
	if format == "json" {
		encoder := json.NewEncoder(writer)
		encoder.SetIndent("", "  ")
		return encoder.Encode(runs)
	}
	if format == "jsonl" {
		for _, run := range runs {
			if err := report.Write(run, format, writer); err != nil {
				return err
			}
		}
		return nil
	}
	seen := make(map[string]struct{})
	for _, run := range runs {
		for _, finding := range run.Findings {
			seen[finding.Email] = struct{}{}
		}
	}
	values := make([]string, 0, len(seen))
	for email := range seen {
		values = append(values, email)
	}
	sort.Strings(values)
	for _, email := range values {
		if _, err := fmt.Fprintln(writer, email); err != nil {
			return err
		}
	}
	return nil
}
