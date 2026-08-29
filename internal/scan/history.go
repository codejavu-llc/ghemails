package scan

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codejavu-llc/ghemails/internal/model"
)

const historyMarker = "GHEMAILS_COMMIT\t"

func (s *Scanner) scanGitHistory(ctx context.Context) (outcome, error) {
	result := outcome{candidates: len(s.candidates)}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return result, fmt.Errorf("deep mode requires git: %w", err)
	}
	tempRoot, err := os.MkdirTemp("", "ghemails-history-*")
	if err != nil {
		return result, err
	}
	defer os.RemoveAll(tempRoot)
	for index, fullName := range SortedKeys(s.candidates) {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		repo := s.candidates[fullName]
		cloneURL := repo.CloneURL
		if cloneURL == "" {
			cloneURL = strings.TrimRight(repo.HTMLURL, "/") + ".git"
		}
		if cloneURL == ".git" {
			cloneURL = "https://github.com/" + fullName + ".git"
		}
		if !safeCloneURL(cloneURL, repo.HTMLURL) {
			result.partial = joinReason(result.partial, "unsafe-clone-url")
			continue
		}
		if s.progress != nil {
			s.progress(fmt.Sprintf("deep history %d/%d: %s", index+1, len(s.candidates), fullName))
		}
		repoDir := filepath.Join(tempRoot, strconv.Itoa(index)+".git")
		repoCtx, cancel := context.WithTimeout(ctx, s.config.HistoryTimeout)
		cloneErr := runGit(repoCtx, gitPath, "", s.config.GitToken, "clone", "--mirror", "--filter=blob:none", "--", cloneURL, repoDir)
		cancel()
		if cloneErr != nil {
			result.partial = joinReason(result.partial, "clone-failed")
			continue
		}
		htmlURL := repo.HTMLURL
		if htmlURL == "" {
			htmlURL = strings.TrimSuffix(cloneURL, ".git")
		}
		partial, scanErr := s.scanGitLogRepository(ctx, gitPath, repoDir, fullName, htmlURL)
		if partial != "" {
			result.partial = joinReason(result.partial, partial)
		}
		if scanErr != nil {
			result.partial = joinReason(result.partial, "history-scan-failed")
		}
		if err := os.RemoveAll(repoDir); err != nil {
			result.partial = joinReason(result.partial, "temporary-cleanup-failed")
		}
		if repo.HasWiki {
			wikiURL := strings.TrimSuffix(cloneURL, ".git") + ".wiki.git"
			wikiDir := filepath.Join(tempRoot, strconv.Itoa(index)+".wiki.git")
			wikiCtx, wikiCancel := context.WithTimeout(ctx, s.config.HistoryTimeout)
			wikiCloneErr := runGit(wikiCtx, gitPath, "", s.config.GitToken, "clone", "--mirror", "--filter=blob:none", "--", wikiURL, wikiDir)
			wikiCancel()
			if wikiCloneErr != nil {
				result.partial = joinReason(result.partial, "wiki-clone-failed")
			} else {
				partial, wikiScanErr := s.scanGitLogRepository(ctx, gitPath, wikiDir, fullName+".wiki", strings.TrimRight(htmlURL, "/")+"/wiki")
				if partial != "" {
					result.partial = joinReason(result.partial, partial)
				}
				if wikiScanErr != nil {
					result.partial = joinReason(result.partial, "wiki-history-scan-failed")
				}
			}
			if err := os.RemoveAll(wikiDir); err != nil {
				result.partial = joinReason(result.partial, "temporary-cleanup-failed")
			}
		}
	}
	return result, nil
}

// scanGitLogRepository is separated from cloning so the parser can be exercised with fixture repositories.
func (s *Scanner) scanGitLogRepository(parent context.Context, gitPath, repoDir, fullName, htmlURL string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, s.config.HistoryTimeout)
	defer cancel()
	args := secureGitArgs(repoDir, "log", "--all", "--no-color", "--no-ext-diff", "--format="+historyMarker+"%H%x09%aE%x09%cE%x09%aI%x09%cI%n%B", "-p")
	cmd := exec.CommandContext(ctx, gitPath, args...)
	cmd.Env = secureGitEnv(s.config.GitToken)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	var stderr limitedBuffer
	stderr.limit = 64 << 10
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var consumed int64
	var sha, path string
	inMessage := false
	for scanner.Scan() {
		line := scanner.Text()
		consumed += int64(len(line) + 1)
		if consumed > s.config.MaxHistoryBytes {
			cancel()
			_ = cmd.Wait()
			return "max-history-bytes", nil
		}
		if strings.HasPrefix(line, historyMarker) {
			fields := strings.Split(strings.TrimPrefix(line, historyMarker), "\t")
			if len(fields) >= 5 {
				sha = fields[0]
				baseURL := strings.TrimRight(htmlURL, "/") + "/commit/" + sha
				s.addText(fields[1], model.Evidence{Source: "git_history_author", URL: baseURL, Repository: fullName, CommitSHA: sha, ObservedAt: model.TimeValue(parseGitDate(fields[3]))})
				s.addText(fields[2], model.Evidence{Source: "git_history_committer", URL: baseURL, Repository: fullName, CommitSHA: sha, ObservedAt: model.TimeValue(parseGitDate(fields[4]))})
			}
			path = ""
			inMessage = true
			continue
		}
		if strings.HasPrefix(line, "diff --git ") {
			inMessage = false
			continue
		}
		if inMessage {
			s.addText(line, model.Evidence{Source: "git_history_message", URL: strings.TrimRight(htmlURL, "/") + "/commit/" + sha, Repository: fullName, CommitSHA: sha})
			continue
		}
		if strings.HasPrefix(line, "+++ b/") || strings.HasPrefix(line, "--- a/") {
			path = line[6:]
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			url := strings.TrimRight(htmlURL, "/") + "/commit/" + sha
			s.addText(line[1:], model.Evidence{Source: "git_blob_history", URL: url, Repository: fullName, CommitSHA: sha, Path: path})
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if ctx.Err() != nil && parent.Err() == nil {
		return "history-timeout", nil
	}
	if scanErr != nil {
		return "", scanErr
	}
	if waitErr != nil {
		return "", fmt.Errorf("git log: %w: %s", waitErr, stderr.String())
	}
	return "", nil
}

func safeCloneURL(value, repositoryHTMLURL string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(parsed.Host, "\r\n") {
		return false
	}
	if repositoryHTMLURL == "" {
		return strings.EqualFold(canonicalHTTPSOrigin(parsed), "github.com:443")
	}
	html, err := url.Parse(repositoryHTMLURL)
	return err == nil && html.Scheme == "https" && html.User == nil && strings.EqualFold(canonicalHTTPSOrigin(parsed), canonicalHTTPSOrigin(html))
}

func canonicalHTTPSOrigin(parsed *url.URL) string {
	port := parsed.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(parsed.Hostname()) + ":" + port
}

func runGit(ctx context.Context, gitPath, repoDir, token string, args ...string) error {
	cmdArgs := secureGitArgs(repoDir, args...)
	cmd := exec.CommandContext(ctx, gitPath, cmdArgs...)
	cmd.Env = secureGitEnv(token)
	var output limitedBuffer
	output.limit = 64 << 10
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, output.String())
	}
	return nil
}

func secureGitArgs(repoDir string, args ...string) []string {
	result := []string{"-c", "protocol.file.allow=never", "-c", "core.hooksPath=/dev/null", "-c", "credential.helper="}
	if repoDir != "" {
		result = append(result, "-C", repoDir)
	}
	return append(result, args...)
}

func secureGitEnv(token string) []string {
	environment := make([]string, 0, len(os.Environ())+8)
	for _, variable := range os.Environ() {
		name := strings.SplitN(variable, "=", 2)[0]
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		environment = append(environment, variable)
	}
	environment = append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=")
	if token != "" {
		environment = append(environment,
			"GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: Bearer "+token,
			"GIT_CONFIG_KEY_1=http.followRedirects", "GIT_CONFIG_VALUE_1=false",
		)
	}
	return environment
}

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	if b.limit <= b.Len() {
		return original, nil
	}
	remaining := b.limit - b.Len()
	if len(value) > remaining {
		value = value[:remaining]
	}
	_, _ = b.Buffer.Write(value)
	return original, nil
}

var _ io.Writer = (*limitedBuffer)(nil)
