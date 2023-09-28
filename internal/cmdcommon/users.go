package cmdcommon

import (
	"fmt"
	"os"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/core"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

const (
	maxResults = 100
	lineBreak  = "----------"

	optionSearch = "[Search...]"
	optionCancel = "Cancel"

	OptionMe      = "Me"
	OptionDefault = "Default"
	OptionNone    = "No-one"
)

type UserLookup struct {
	Client            *jira.Client
	Project           string
	AdditionalOptions []string // e.g. OptionDefault

	users         []*jira.User
	searchKeyword string
}

func CreateUserLookup(client *jira.Client, project string, additionalOptions ...string) UserLookup {
	return UserLookup{client, project, additionalOptions, nil, ""}
}

// setAssignee
func (ul *UserLookup) SelectUser(prompt string) (string, error) {
	if ul.users == nil {
		err := ul.init()
		if err != nil {
			return "", err
		}
	}

	var (
		ans  string
		last bool
	)

	for {
		qs := &survey.Question{
			Name: "user",
			Prompt: &survey.Select{
				Message: prompt,
				Help:    "Can't find the user? Select search and look for a keyword or cancel to abort",
				Options: ul.getOptions(last),
			},
			Validate: func(val interface{}) error {
				errInvalidSelection := fmt.Errorf("invalid selection")

				ans, ok := val.(core.OptionAnswer)
				if !ok {
					return errInvalidSelection
				}
				if ans.Value == "" || ans.Value == lineBreak {
					return errInvalidSelection
				}

				return nil
			},
		}

		if err := survey.Ask([]*survey.Question{qs}, &ans); err != nil {
			return "", err
		}
		if ans == optionCancel {
			cmdutil.Fail("Action aborted")
			os.Exit(0)
		}
		if ans != optionSearch {
			break
		}
		if err := ul.getSearchKeyword(); err != nil {
			return "", err
		}
		if err := ul.updateUsersList(); err != nil {
			return "", err
		}
		last = true
	}

	return ans, nil
}

func (ul *UserLookup) getOptions(last bool) []string {
	var validUsers []string

	for _, t := range ul.users {
		if t.Active {
			validUsers = append(validUsers, getFullName(t))
		}
	}

	options := []string{optionSearch}

	if last {
		options = append(options, validUsers...)
		options = append(options, lineBreak)
		options = append(options, ul.AdditionalOptions...)
		options = append(options, optionCancel)
	} else {
		options = append(options, ul.AdditionalOptions...)
		options = append(options, optionCancel)
		options = append(options, lineBreak)
		options = append(options, validUsers...)
	}

	return options
}

func (ul *UserLookup) getSearchKeyword() error {
	qs := &survey.Question{
		Name: "user",
		Prompt: &survey.Input{
			Message: "Search user:",
			Help:    "Type user email or display name to search for a user",
		},
		Validate: func(val interface{}) error {
			errInvalidKeyword := fmt.Errorf("enter at least 3 characters to search")

			str, ok := val.(string)
			if !ok {
				return errInvalidKeyword
			}
			if len(str) < 3 {
				return errInvalidKeyword
			}

			return nil
		},
	}
	return survey.Ask([]*survey.Question{qs}, &ul.searchKeyword)
}

// searchAndAssignUser
func (ul *UserLookup) updateUsersList() error {
	u, err := api.ProxyUserSearch(ul.Client, &jira.UserSearchOptions{
		Query:      ul.searchKeyword,
		Project:    ul.Project,
		MaxResults: maxResults,
	})
	if err != nil {
		return fmt.Errorf("failed to fetch users: %w", err)
	}
	ul.users = u
	return nil
}

// setAvailableUsers
func (ul *UserLookup) init() error {
	s := cmdutil.Info("Fetching available users. Please wait...")
	defer s.Stop()

	return ul.updateUsersList()
}

// verifyAssignee
func (ul *UserLookup) FindUser(query string) (*jira.User, error) {
	if ul.users == nil {
		err := ul.init()
		if err != nil {
			return nil, err
		}
	}

	var user *jira.User

	query = strings.ToLower(query)

	for _, u := range ul.users {
		if query == strings.ToLower(u.DisplayName) ||
			query == strings.ToLower(u.Name) ||
			query == strings.ToLower(u.Email) ||
			query == strings.ToLower(getFullName(u)) {
			user = u
			break
		}
	}

	if user == nil {
		return nil, fmt.Errorf("user not found %q", query)
	}
	return user, nil
}

func (ul *UserLookup) FindMe() (*jira.User, error) {
	me, err := ul.Client.Me()
	if err != nil {
		return nil, err
	}
	user := jira.User{
		AccountID: "",
		Email: me.Email,
		Name: me.Login,
		DisplayName: me.Name,
		Active: true,
	}
	return &user, nil
}

func getFullName(user *jira.User) string {
	name := user.DisplayName
	if user.Name != "" {
		name += fmt.Sprintf(" (%s)", user.Name)
	}
	return name
}
