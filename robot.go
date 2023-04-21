package main

import (
	"fmt"
	"regexp"

	sdk "github.com/google/go-github/v36/github"
	gc "github.com/opensourceways/robot-github-lib/client"
	"github.com/opensourceways/robot-github-lib/framework"
	"github.com/opensourceways/server-common-lib/config"
	"github.com/sirupsen/logrus"
)

const (
	botName              = "lifecycle"
	optionFailureMessage = `***@%s*** you can't %s it unless you are the author of it or a collaborator.`
	createAction         = "created"
)

var (
	reopenRe = regexp.MustCompile(`(?mi)^/reopen\s*$`)
	closeRe  = regexp.MustCompile(`(?mi)^/close\s*$`)
)

type iClient interface {
	UpdatePR(pr gc.PRInfo, request *sdk.PullRequest) (*sdk.PullRequest, error)
	UpdateIssue(is gc.PRInfo, iss *sdk.IssueRequest) error
	CreateIssueComment(is gc.PRInfo, comment string) error
	IsCollaborator(pr gc.PRInfo, login string) (bool, error)
	ClosePR(pr gc.PRInfo) error
	ReopenPR(pr gc.PRInfo) error
	CloseIssue(pr gc.PRInfo) error
	ReopenIssue(pr gc.PRInfo) error
}

func newRobot(cli iClient) *robot {
	return &robot{cli: cli}
}

type robot struct {
	cli iClient
}

func (bot *robot) NewConfig() config.Config {
	return &configuration{}
}

func (bot *robot) getConfig(cfg config.Config, org, repo string) (*botConfig, error) {
	c, ok := cfg.(*configuration)
	if !ok {
		return nil, fmt.Errorf("can't convert to configuration")
	}

	if bc := c.configFor(org, repo); bc != nil {
		return bc, nil
	}

	return nil, fmt.Errorf("no config for this repo:%s/%s", org, repo)
}

func (bot *robot) RegisterEventHandler(f framework.HandlerRegister) {
	f.RegisterIssueCommentHandler(bot.handleIssueCommentEvent)
}

func (bot *robot) handleIssueCommentEvent(e *sdk.IssueCommentEvent, cfg config.Config, log *logrus.Entry) error {
	if e.GetAction() != createAction {
		return nil
	}
	org, repo := gc.GetOrgRepo(e.GetRepo())
	c, err := bot.getConfig(cfg, org, repo)
	if err != nil {
		return err
	}

	if c == nil {
		log.Errorf("can not get config for bot %v", err)
		return nil
	}
	info := gc.PRInfo{Org: org, Repo: repo, Number: e.GetIssue().GetNumber()}

	return bot.handleLifeCycle(e, info)
}

func (bot *robot) handleLifeCycle(e *sdk.IssueCommentEvent, p gc.PRInfo) error {
	author := e.GetIssue().GetUser().GetLogin()
	comment := e.GetComment().GetBody()
	commenter := e.GetComment().GetUser().GetLogin()

	if e.GetIssue().GetState() == "closed" && reopenRe.MatchString(comment) {
		return bot.open(e, p, commenter, author)
	}

	if e.GetIssue().GetState() == "open" && closeRe.MatchString(comment) {
		return bot.close(e, p, commenter, author)
	}

	return nil
}

func (bot *robot) open(e *sdk.IssueCommentEvent, p gc.PRInfo, commenter, author string) error {
	v, err := bot.hasPermission(p, commenter, author)
	if err != nil {
		return err
	}
	if !v {
		return bot.cli.CreateIssueComment(p, fmt.Sprintf(optionFailureMessage, commenter, "reopen"))
	}

	if e.GetIssue().IsPullRequest() {
		return bot.cli.ReopenPR(p)
	}

	return bot.cli.ReopenIssue(p)
}

func (bot *robot) close(e *sdk.IssueCommentEvent, p gc.PRInfo, commenter, author string) error {
	v, err := bot.hasPermission(p, commenter, author)
	if err != nil {
		return err
	}
	if !v {
		return bot.cli.CreateIssueComment(p, fmt.Sprintf(optionFailureMessage, commenter, "reopen"))
	}

	if e.GetIssue().IsPullRequest() {
		return bot.cli.ClosePR(p)
	}

	return bot.cli.CloseIssue(p)
}

func (bot *robot) hasPermission(p gc.PRInfo, commenter, author string) (bool, error) {
	if commenter == author {
		return true, nil
	}

	return bot.cli.IsCollaborator(p, commenter)
}
