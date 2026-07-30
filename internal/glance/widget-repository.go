package glance

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var repositoryWidgetTemplate = mustParseTemplate("repository.html", "widget-base.html")

type repositoryWidget struct {
	widgetBase               `yaml:",inline"`
	RequestedRepository      string       `yaml:"repository"`
	RequestedRepositories    []string     `yaml:"repositories"`
	Token                    string       `yaml:"token"`
	PullRequestsLimit        int          `yaml:"pull-requests-limit"`
	IssuesLimit              int          `yaml:"issues-limit"`
	CommitsLimit             int          `yaml:"commits-limit"`
	CommitLabelTemplate      string       `yaml:"commit-label-template"`
	PullRequestLabelTemplate string       `yaml:"pull-request-label-template"`
	IssueLabelTemplate       string       `yaml:"issue-label-template"`
	Repositories             []repository `yaml:"-"`
}

func (widget *repositoryWidget) initialize() error {
	widget.withTitle("Repository").withCacheDuration(1 * time.Hour)

	if len(widget.RequestedRepositories) == 0 && widget.RequestedRepository != "" {
		widget.RequestedRepositories = []string{widget.RequestedRepository}
	}

	if len(widget.RequestedRepositories) == 0 {
		return errors.New("no repository or repositories specified")
	}

	if widget.PullRequestsLimit == 0 || widget.PullRequestsLimit < -1 {
		widget.PullRequestsLimit = 3
	}

	if widget.IssuesLimit == 0 || widget.IssuesLimit < -1 {
		widget.IssuesLimit = 3
	}

	if widget.CommitsLimit == 0 || widget.CommitsLimit < -1 {
		widget.CommitsLimit = -1
	}

	if widget.CommitLabelTemplate == "" {
		widget.CommitLabelTemplate = "{MESSAGE}"
	}

	if widget.PullRequestLabelTemplate == "" {
		widget.PullRequestLabelTemplate = "{TITLE}"
	}

	if widget.IssueLabelTemplate == "" {
		widget.IssueLabelTemplate = "{TITLE}"
	}

	return nil
}

func (widget *repositoryWidget) update(ctx context.Context) {
	requests := make([]*repositoryFetchRequest, len(widget.RequestedRepositories))
	for i, repo := range widget.RequestedRepositories {
		requests[i] = &repositoryFetchRequest{
			repository: repo,
			token:      widget.Token,
			maxPRs:     widget.PullRequestsLimit,
			maxIssues:  widget.IssuesLimit,
			maxCommits: widget.CommitsLimit,
		}
	}

	details, err := fetchRepositoriesDetailsFromGithub(requests)
	if !widget.canContinueUpdateAfterHandlingErr(err) {
		return
	}

	for i := range details {
		widget.applyLabelTemplates(&details[i])
	}

	widget.Repositories = details
}

func (widget *repositoryWidget) applyLabelTemplates(repo *repository) {
	for i := range repo.Commits {
		c := &repo.Commits[i]
		c.Label = strings.ReplaceAll(widget.CommitLabelTemplate, "{SHA}", c.Sha)
		c.Label = strings.ReplaceAll(c.Label, "{AUTHOR}", c.Author)
		c.Label = strings.ReplaceAll(c.Label, "{MESSAGE}", c.Message)
	}

	for i := range repo.PullRequests {
		pr := &repo.PullRequests[i]
		pr.Label = strings.ReplaceAll(widget.PullRequestLabelTemplate, "{NUMBER}", strconv.Itoa(pr.Number))
		pr.Label = strings.ReplaceAll(pr.Label, "{TITLE}", pr.Title)
		pr.Label = strings.ReplaceAll(pr.Label, "{AUTHOR}", pr.Author)
	}

	for i := range repo.Issues {
		issue := &repo.Issues[i]
		issue.Label = strings.ReplaceAll(widget.IssueLabelTemplate, "{NUMBER}", strconv.Itoa(issue.Number))
		issue.Label = strings.ReplaceAll(issue.Label, "{TITLE}", issue.Title)
		issue.Label = strings.ReplaceAll(issue.Label, "{AUTHOR}", issue.Author)
	}
}

func (widget *repositoryWidget) Render() template.HTML {
	return widget.renderTemplate(widget, repositoryWidgetTemplate)
}

type repositoryFetchRequest struct {
	repository string
	token      string
	maxPRs     int
	maxIssues  int
	maxCommits int
}

type repository struct {
	Name             string
	Stars            int
	Forks            int
	OpenPullRequests int
	PullRequests     []githubTicket
	OpenIssues       int
	Issues           []githubTicket
	LastCommits      int
	Commits          []githubCommitDetails
}

type githubTicket struct {
	Number    int
	CreatedAt time.Time
	Title     string
	Author    string
	Label     string
}

type githubCommitDetails struct {
	Sha       string
	Author    string
	CreatedAt time.Time
	Message   string
	Label     string
}

type githubRepositoryResponseJson struct {
	Name  string `json:"full_name"`
	Stars int    `json:"stargazers_count"`
	Forks int    `json:"forks_count"`
}

type githubTicketResponseJson struct {
	Count   int `json:"total_count"`
	Tickets []struct {
		Number    int    `json:"number"`
		CreatedAt string `json:"created_at"`
		Title     string `json:"title"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"items"`
}

type gitHubCommitResponseJson struct {
	Sha    string `json:"sha"`
	Commit struct {
		Author struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

func fetchRepositoriesDetailsFromGithub(requests []*repositoryFetchRequest) ([]repository, error) {
	job := newJob(fetchRepositoryDetailsTask, requests).withWorkers(10)
	results, errs, err := workerPoolDo(job)
	if err != nil {
		return nil, err
	}

	var failed int
	var partial bool
	repositories := make([]repository, 0, len(requests))

	for i := range results {
		if errs[i] != nil {
			if errors.Is(errs[i], errPartialContent) {
				partial = true
				repositories = append(repositories, results[i])
				continue
			}

			failed++
			slog.Error("Failed to fetch repository", "repository", requests[i].repository, "error", errs[i])
			continue
		}

		repositories = append(repositories, results[i])
	}

	if failed == len(requests) {
		return nil, errNoContent
	}

	if failed > 0 || partial {
		return repositories, fmt.Errorf("%w: could not get some repository data", errPartialContent)
	}

	return repositories, nil
}

func fetchRepositoryDetailsTask(request *repositoryFetchRequest) (repository, error) {
	return fetchRepositoryDetailsFromGithub(
		request.repository,
		request.token,
		request.maxPRs,
		request.maxIssues,
		request.maxCommits,
	)
}

func fetchRepositoryDetailsFromGithub(repo string, token string, maxPRs int, maxIssues int, maxCommits int) (repository, error) {
	repositoryRequest, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s", repo), nil)
	if err != nil {
		return repository{}, fmt.Errorf("%w: could not create request with repository: %v", errNoContent, err)
	}

	PRsRequest, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/search/issues?q=is:pr+is:open+repo:%s&per_page=%d", repo, maxPRs), nil)
	issuesRequest, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/search/issues?q=is:issue+is:open+repo:%s&per_page=%d", repo, maxIssues), nil)
	CommitsRequest, _ := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/commits?per_page=%d", repo, maxCommits), nil)

	if token != "" {
		token = fmt.Sprintf("Bearer %s", token)
		repositoryRequest.Header.Add("Authorization", token)
		PRsRequest.Header.Add("Authorization", token)
		issuesRequest.Header.Add("Authorization", token)
		CommitsRequest.Header.Add("Authorization", token)
	}

	var repositoryResponse githubRepositoryResponseJson
	var detailsErr error
	var PRsResponse githubTicketResponseJson
	var PRsErr error
	var issuesResponse githubTicketResponseJson
	var issuesErr error
	var commitsResponse []gitHubCommitResponseJson
	var CommitsErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go (func() {
		defer wg.Done()
		repositoryResponse, detailsErr = decodeJsonFromRequest[githubRepositoryResponseJson](defaultHTTPClient, repositoryRequest)
	})()

	if maxPRs > 0 {
		wg.Add(1)
		go (func() {
			defer wg.Done()
			PRsResponse, PRsErr = decodeJsonFromRequest[githubTicketResponseJson](defaultHTTPClient, PRsRequest)
		})()
	}

	if maxIssues > 0 {
		wg.Add(1)
		go (func() {
			defer wg.Done()
			issuesResponse, issuesErr = decodeJsonFromRequest[githubTicketResponseJson](defaultHTTPClient, issuesRequest)
		})()
	}

	if maxCommits > 0 {
		wg.Add(1)
		go (func() {
			defer wg.Done()
			commitsResponse, CommitsErr = decodeJsonFromRequest[[]gitHubCommitResponseJson](defaultHTTPClient, CommitsRequest)
		})()
	}

	wg.Wait()

	if detailsErr != nil {
		return repository{}, fmt.Errorf("%w: could not get repository details: %s", errNoContent, detailsErr)
	}

	details := repository{
		Name:         repositoryResponse.Name,
		Stars:        repositoryResponse.Stars,
		Forks:        repositoryResponse.Forks,
		PullRequests: make([]githubTicket, 0, len(PRsResponse.Tickets)),
		Issues:       make([]githubTicket, 0, len(issuesResponse.Tickets)),
		Commits:      make([]githubCommitDetails, 0, len(commitsResponse)),
	}

	err = nil

	if maxPRs > 0 {
		if PRsErr != nil {
			err = fmt.Errorf("%w: could not get PRs: %s", errPartialContent, PRsErr)
		} else {
			details.OpenPullRequests = PRsResponse.Count

			for i := range PRsResponse.Tickets {
				details.PullRequests = append(details.PullRequests, githubTicket{
					Number:    PRsResponse.Tickets[i].Number,
					CreatedAt: parseRFC3339Time(PRsResponse.Tickets[i].CreatedAt),
					Title:     PRsResponse.Tickets[i].Title,
					Author:    PRsResponse.Tickets[i].User.Login,
				})
			}
		}
	}

	if maxIssues > 0 {
		if issuesErr != nil {
			// TODO: fix, overwriting the previous error
			err = fmt.Errorf("%w: could not get issues: %s", errPartialContent, issuesErr)
		} else {
			details.OpenIssues = issuesResponse.Count

			for i := range issuesResponse.Tickets {
				details.Issues = append(details.Issues, githubTicket{
					Number:    issuesResponse.Tickets[i].Number,
					CreatedAt: parseRFC3339Time(issuesResponse.Tickets[i].CreatedAt),
					Title:     issuesResponse.Tickets[i].Title,
					Author:    issuesResponse.Tickets[i].User.Login,
				})
			}
		}
	}

	if maxCommits > 0 {
		if CommitsErr != nil {
			err = fmt.Errorf("%w: could not get commits: %s", errPartialContent, CommitsErr)
		} else {
			for i := range commitsResponse {
				details.Commits = append(details.Commits, githubCommitDetails{
					Sha:       commitsResponse[i].Sha,
					Author:    commitsResponse[i].Commit.Author.Name,
					CreatedAt: parseRFC3339Time(commitsResponse[i].Commit.Author.Date),
					Message:   strings.SplitN(commitsResponse[i].Commit.Message, "\n\n", 2)[0],
				})
			}
		}
	}

	return details, err
}
