package tclog

import (
	"fmt"
	"os"
	"strconv"

	"github.com/AlecAivazis/survey/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/internal/query"
	"github.com/ankitpokhrel/jira-cli/internal/teamcity"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

const (
	helpText = `Download a log file from TeamCity and attach it to an issue.`
	examples = `$ jira issue tclog ISSUE-1 863155 usertest_addcols.log

# Attach a log file from TeamCity build 863155
$ jira issue tclog ISSUE-1 863155 usertest_addcols.log`
)

// NewCmdTclog is a tclog command.
func NewCmdTclog() *cobra.Command {
	cmd := cobra.Command{
		Use:     "tclog ISSUE-KEY BUILD_NUMBER FILENAME",
		Short:   "Attach log file to issue",
		Long:    helpText,
		Example: examples,
		Annotations: map[string]string{
			"help:args": "ISSUE-KEY\tIssue key, eg: ISSUE-1\n" +
				"BUILD_NUMBER\tBuild number from the TeamCity URL (not the UI)\n" +
				"FILENAME\tName of the log file to download from TeamCity and attach to the issue",
		},
		Run: tclog,
	}

	return &cmd
}

func tclog(cmd *cobra.Command, args []string) {
	teamcity_api_token := getTeamCityApiToken()
	project := viper.GetString("project.key")
	params, err := parseArgsAndFlags(cmd.Flags(), args, project)
	cmdutil.ExitIfError(err)

	client := api.DefaultClient(params.debug)
	mc := tclogCmd{
		client: client,
		params: params,
	}

	cmdutil.ExitIfError(mc.setIssueKey(project))

	err = func() error {
		s := cmdutil.Info(fmt.Sprintf("Locating log file %v", params.filename))
		defer func() { s.Stop() }()
		tc := teamcity.TeamCity{
			URL:         "https://xpress-teamcity.xpressdev.aws.fico.com",
			AccessToken: teamcity_api_token,
		}
		path, err := tc.FindArtifact(params.buildnum, params.filename)
		if err != nil {
			return err
		}
		s.Stop()
		s = cmdutil.Info(fmt.Sprintf("Downloading log file %v", path))
		buf, err := tc.DownloadArtifact(params.buildnum, path)
		if err != nil {
			return err
		}
		s.Stop()
		s = cmdutil.Info(fmt.Sprintf("Uploading attachment to issue %q", mc.params.key))

		return client.AttachToIssue(mc.params.key, mc.params.filename, buf)
	}()
	cmdutil.ExitIfError(err)

	cmdutil.Success(fmt.Sprintf("Issue %q updated successfully", mc.params.key))
}

type tclogParams struct {
	key      string
	buildnum int
	filename string
	debug    bool
}

func parseArgsAndFlags(flags query.FlagParser, args []string, project string) (*tclogParams, error) {
	var key, filename string
	var buildnum int

	nargs := len(args)
	if nargs >= 1 {
		key = cmdutil.GetJiraIssueKey(project, args[0])
	}
	if nargs >= 2 {
		ival, err := strconv.Atoi(args[1])
		if err != nil {
			return nil, fmt.Errorf("build number must be a number")
		}
		buildnum = ival
	}
	if nargs >= 3 {
		filename = args[2]
	}

	debug, err := flags.GetBool("debug")
	cmdutil.ExitIfError(err)

	return &tclogParams{
		key:      key,
		buildnum: buildnum,
		filename: filename,
		debug:    debug,
	}, nil
}

type tclogCmd struct {
	client *jira.Client
	params *tclogParams
}

func (mc *tclogCmd) setIssueKey(project string) error {
	if mc.params.key != "" {
		return nil
	}

	var ans string

	qs := &survey.Question{
		Name:     "key",
		Prompt:   &survey.Input{Message: "Issue key"},
		Validate: survey.Required,
	}
	if err := survey.Ask([]*survey.Question{qs}, &ans); err != nil {
		return err
	}
	mc.params.key = cmdutil.GetJiraIssueKey(project, ans)

	return nil
}

func getTeamCityApiToken() (string) {
	api_token := os.Getenv("TEAMCITY_API_TOKEN")
	if api_token != "" {
		return api_token
	}

	cmdutil.Warn(`The tool needs a TeamCity API token to function.

Follow this link and click "Create access token": https://xpress-teamcity.xpressdev.aws.fico.com/profile.html?item=accessTokens

After generating the API token, export it to your shell as a TEAMCITY_API_TOKEN env variable
`)

	os.Exit(1)
	return ""
}
