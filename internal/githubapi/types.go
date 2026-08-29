package githubapi

import "time"

type TextMatch struct {
	ObjectURL string `json:"object_url"`
	Property  string `json:"property"`
	Fragment  string `json:"fragment"`
}

type Repository struct {
	FullName      string    `json:"full_name"`
	HTMLURL       string    `json:"html_url"`
	CloneURL      string    `json:"clone_url"`
	Description   string    `json:"description"`
	DefaultBranch string    `json:"default_branch"`
	Archived      bool      `json:"archived"`
	Fork          bool      `json:"fork"`
	Private       bool      `json:"private"`
	HasWiki       bool      `json:"has_wiki"`
	Visibility    string    `json:"visibility"`
	PushedAt      time.Time `json:"pushed_at"`
}

type CodeSearchResponse struct {
	TotalCount        int        `json:"total_count"`
	IncompleteResults bool       `json:"incomplete_results"`
	Items             []CodeItem `json:"items"`
}

type CodeItem struct {
	Name        string      `json:"name"`
	Path        string      `json:"path"`
	SHA         string      `json:"sha"`
	URL         string      `json:"url"`
	HTMLURL     string      `json:"html_url"`
	Repository  Repository  `json:"repository"`
	TextMatches []TextMatch `json:"text_matches"`
}

type IssueSearchResponse struct {
	TotalCount        int         `json:"total_count"`
	IncompleteResults bool        `json:"incomplete_results"`
	Items             []IssueItem `json:"items"`
}

type IssueItem struct {
	Number        int         `json:"number"`
	HTMLURL       string      `json:"html_url"`
	Title         string      `json:"title"`
	Body          string      `json:"body"`
	RepositoryURL string      `json:"repository_url"`
	User          User        `json:"user"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	TextMatches   []TextMatch `json:"text_matches"`
}

type CommitSearchResponse struct {
	TotalCount        int          `json:"total_count"`
	IncompleteResults bool         `json:"incomplete_results"`
	Items             []CommitItem `json:"items"`
}

type Signature struct {
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Date  time.Time `json:"date"`
}

type GitCommit struct {
	Author    Signature `json:"author"`
	Committer Signature `json:"committer"`
	Message   string    `json:"message"`
}

type CommitItem struct {
	SHA        string     `json:"sha"`
	HTMLURL    string     `json:"html_url"`
	Commit     GitCommit  `json:"commit"`
	Author     *User      `json:"author"`
	Committer  *User      `json:"committer"`
	Repository Repository `json:"repository"`
}

type RepositorySearchResponse struct {
	TotalCount        int          `json:"total_count"`
	IncompleteResults bool         `json:"incomplete_results"`
	Items             []Repository `json:"items"`
}

type User struct {
	Login   string `json:"login"`
	HTMLURL string `json:"html_url"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Company string `json:"company"`
	Bio     string `json:"bio"`
	Blog    string `json:"blog"`
}

type Event struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Repo      struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"repo"`
	Payload any `json:"payload"`
}
