package script

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/ankitpokhrel/jira-cli/api"
	"github.com/ankitpokhrel/jira-cli/internal/cmd/issue/clone"
	"github.com/ankitpokhrel/jira-cli/internal/cmd/issue/create"
	"github.com/ankitpokhrel/jira-cli/internal/cmdcommon"
	"github.com/ankitpokhrel/jira-cli/internal/cmdutil"
	"github.com/ankitpokhrel/jira-cli/pkg/jira"
)

const (
	helpText = `Execute a script containing issue commands.`
	examples = `$ jira issue script create_defect.yml arg1 arg2

# Run a script which takes two arguments
$ jira issue script create_defect.yml arg1 arg2`
)

// NewCmdScript is a script command.
func NewCmdScript() *cobra.Command {
	cmd := cobra.Command{
		Use:     "script FILENAME ARGS...",
		Short:   "Execute a script",
		Long:    helpText,
		Example: examples,
		Annotations: map[string]string{
			"help:args": "FILENAME\tName of the script to execute\n" +
				"ARGS\tOther arguments required by the script",
		},
		Run: script,
	}

	cmd.Flags().Bool("no-input", false, "Disable prompt for non-required fields")

	return &cmd
}

func script(cmd *cobra.Command, args []string) {
	if len(args) < 1 {
		cmdutil.Failed("missing command line argument FILENAME")
	}

	cmdutil.ExitIfError(runScript(cmd, args[0], args[1:]))
}

type Script struct {
	Define  map[string]string `yaml:"define"`
	Actions []map[string]any  `yaml:"actions"`
}

var varRe = regexp.MustCompile(`\$\w+`)

func interpolateVars(o any, define map[string]string) (any, error) {
	switch obj := o.(type) {
	case string:
		newString := ""
		lastIdx := 0
		for _, match := range varRe.FindAllStringIndex(obj, -1) {
			newString += obj[lastIdx:match[0]]
			varName := obj[match[0]+1 : match[1]] // +1 to drop leading $
			val, ok := define[varName]
			if !ok {
				return nil, fmt.Errorf("unknown variable %v in script", varName)
			}
			newString += val
			lastIdx = match[1]
		}
		newString += obj[lastIdx:]
		return newString, nil
	case []any:
		newSlice := make([]any, len(obj))
		for idx, val := range obj {
			newVal, err := interpolateVars(val, define)
			if err != nil {
				return nil, err
			}
			newSlice[idx] = newVal
		}
		return newSlice, nil
	case map[string]any:
		newMap := make(map[string]any)
		for key, val := range obj {
			newVal, err := interpolateVars(val, define)
			if err != nil {
				return nil, err
			}
			newMap[key] = newVal
		}
		return newMap, nil
	default:
		return nil, fmt.Errorf("unexpected script data of type %T", o)
	}
}

func (s *Script) Interpolate() error {
	for idx, action := range s.Actions {
		newMap, err := interpolateVars(action, s.Define)
		if err != nil {
			return err
		}
		s.Actions[idx] = newMap.(map[string]any)
	}
	return nil
}

func (s *Script) SetupArgs(args []string, ul *cmdcommon.UserLookup) error {
	keys := make([]string, len(s.Define))
	i := 0
	for key := range(s.Define) {
		keys[i] = key
		i++
	}
	slices.SortFunc(keys, func(a, b string) int {return cmp.Compare(s.Define[a], s.Define[b])})

	for _, key := range(keys) {
		val := s.Define[key]
		if val[0] == '~' {
			varname := val[1:]
			if len(varname) > 0 && varname[0] == '~' {
				// Escaped tilde
				s.Define[key] = varname[1:]
			} else if varname == "choose_user" {
				choice, err := ul.SelectUser(fmt.Sprintf("Select user for %v:", key))
				if err != nil {
					return err
				}
				var user *jira.User
				if choice == cmdcommon.OptionMe {
					user, err = ul.FindMe()
					if err != nil {
						return err
					}
				} else {
					user, err = ul.FindUser(choice)
					if err != nil {
						return err
					}
				}
				s.Define[key] = user.Name
			} else if (varname == "me") {
				user, err := ul.FindMe()
				if err != nil {
					return err
				}
				s.Define[key] = user.Name
			} else {
				ival, err := strconv.Atoi(varname)
				if err != nil {
					return fmt.Errorf("unknown built-in variable %v", val)
				}
				if len(args) < ival {
					return fmt.Errorf("missing command line argument %v: %v", ival, key)
				}
				s.Define[key] = args[ival - 1]
			}
		}
	}
	return nil
}

type issueKeyConsumer struct {
	issueKey string
}

func (consumer *issueKeyConsumer) Consume(issueKey string) {
	consumer.issueKey = issueKey
}

func runScript(cmd *cobra.Command, filename string, args []string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	script := Script{}
	dec := yaml.NewDecoder(f)
	err = dec.Decode(&script)
	if err != nil {
		return err
	}

	project := viper.GetString("project.key")
	debug, err := cmd.Flags().GetBool("debug")
	if err != nil {
		return err
	}
	client := api.DefaultClient(debug)
	ul := cmdcommon.CreateUserLookup(client, project, cmdcommon.OptionMe)
	err = script.SetupArgs(args, &ul)
	if err != nil {
		return err
	}

	root := cmd.Root()

	for _, action := range script.Actions {
		nm, ok := action["action"]
		if !ok {
			return fmt.Errorf("script contains an action with no 'action' property")
		}
		name, ok := nm.(string)
		if !ok {
			return fmt.Errorf("script contains an action with an invalid 'action' property: %v", nm)
		}
		actionUntyped, err := interpolateVars(action, script.Define)
		if err != nil {
			return err
		}
		action = actionUntyped.(map[string]any)

		consumer := issueKeyConsumer{}
		args := []string{"issue", name}
		subCmd, _, err := root.Find([]string{"issue", name})
		if err != nil {
			return err
		}
		if name == "create" {
			subCmd.Run = func(c *cobra.Command, _ []string) {
				create.DoCreate(c, &consumer)
			}
		} else if name == "clone" {
			subCmd.Run = func(c *cobra.Command, args []string) {
				clone.DoClone(c, args, &consumer)
			}
		}
		noInput, _ := cmd.Flags().GetBool("no-input")
		if noInput && subCmd.Flags().Lookup("no-input") != nil {
			args = append(args, "--no-input")
		}
		for key, value := range action {
			switch key {
			case "action":
				continue
			case "args":
				argslice, ok := value.([]any)
				if ok {
					// Array of args
					for _, arg := range argslice {
						args = append(args, fmt.Sprint(arg))
					}
				} else {
					// Single arg
					args = append(args, fmt.Sprint(value))
				}
			case "custom":
				customs, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("'custom' should contain a map from custom field name to value")
				}
				for fname, fvalue := range customs {
					mapval, ok := fvalue.(map[string]any)
					if ok {
						// Convert the map to JSON
						bytes, err := json.Marshal(mapval)
						if err != nil {
							return err
						}
						strval := string(bytes)
						args = append(args, "--custom")
						args = append(args, fmt.Sprintf("json:%v=%v", fname, strval))
					} else {
						// Raw custom field value
						args = append(args, "--custom")
						args = append(args, fmt.Sprintf("%v=%v", fname, fvalue))
					}
				}
			default:
				arrval, ok := value.([]any)
				if ok {
					// Array of items: specify the flag several times
					for _, item := range arrval {
						args = append(args, "--"+key)
						args = append(args, fmt.Sprint(item))
					}
				} else {
					// Simple item
					args = append(args, "--"+key)
					args = append(args, fmt.Sprint(value))
				}
			}
		}
		fmt.Printf("Running %v\n", args)
		root.SetArgs(args)
		_, err = root.ExecuteC()
		if err != nil {
			return err
		}
		if name == "create" || name == "clone" {
			script.Define["issue_key"] = consumer.issueKey
		}
	}
	return nil
}
