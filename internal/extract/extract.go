package extract

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/idna"
)

var emailCandidate = regexp.MustCompile(`(?i)[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?`)

var roleAccounts = map[string]struct{}{
	"abuse": {}, "admin": {}, "billing": {}, "careers": {}, "compliance": {}, "contact": {},
	"devops": {}, "help": {}, "hello": {}, "hr": {}, "info": {}, "it": {}, "legal": {},
	"marketing": {}, "noc": {}, "office": {}, "privacy": {}, "sales": {}, "security": {},
	"soc": {}, "support": {}, "team": {}, "webmaster": {},
}

type Matcher struct {
	domain            string
	includeSubdomains bool
	includeNoreply    bool
}

func NormalizeDomain(input string) (string, error) {
	domain := strings.TrimSpace(strings.ToLower(input))
	domain = strings.TrimSuffix(domain, ".")
	if strings.Contains(domain, "://") || strings.ContainsAny(domain, "/:@ ") {
		return "", fmt.Errorf("domain must be a hostname, not a URL or email address")
	}
	ascii, err := idna.Lookup.ToASCII(domain)
	if err != nil {
		return "", fmt.Errorf("invalid internationalized domain: %w", err)
	}
	if len(ascii) == 0 || len(ascii) > 253 || !strings.Contains(ascii, ".") {
		return "", fmt.Errorf("domain must be a valid dotted DNS name")
	}
	for _, label := range strings.Split(ascii, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", fmt.Errorf("invalid DNS label %q", label)
		}
		for _, ch := range label {
			if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' {
				return "", fmt.Errorf("invalid DNS character %q", ch)
			}
		}
	}
	return ascii, nil
}

func NewMatcher(domain string, includeSubdomains, includeNoreply bool) *Matcher {
	return &Matcher{domain: domain, includeSubdomains: includeSubdomains, includeNoreply: includeNoreply}
}

func (m *Matcher) Find(text string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, candidate := range emailCandidate.FindAllString(text, -1) {
		email := strings.ToLower(candidate)
		parts := strings.Split(email, "@")
		if len(parts) != 2 || !validLocal(parts[0]) || len(email) > 254 {
			continue
		}
		domain := strings.TrimSuffix(parts[1], ".")
		matches := domain == m.domain || (m.includeSubdomains && strings.HasSuffix(domain, "."+m.domain))
		if !matches || (!m.includeNoreply && strings.HasSuffix(domain, "users.noreply.github.com")) {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		result = append(result, email)
	}
	return result
}

func validLocal(local string) bool {
	return len(local) > 0 && len(local) <= 64 && local[0] != '.' && local[len(local)-1] != '.' && !strings.Contains(local, "..")
}

func Classification(email string) string {
	local := strings.SplitN(strings.ToLower(email), "@", 2)[0]
	local = strings.SplitN(local, "+", 2)[0]
	if _, ok := roleAccounts[local]; ok {
		return "role"
	}
	return "person"
}
