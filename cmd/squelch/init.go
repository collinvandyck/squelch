package main

import (
	_ "embed"
	"fmt"
	"regexp"
	"text/template"

	"github.com/spf13/cobra"
)

var (
	//go:embed init.zsh
	initZsh string

	initZshTmpl = template.Must(template.New("init.zsh").Parse(initZsh))

	// Restrict alias names to characters that need no quoting in the generated shell code.
	aliasName = regexp.MustCompile(`^[A-Za-z0-9_.+-]+$`)
	// Skip arguments also land unquoted in the generated shell code, inside a subscript.
	skipArg = regexp.MustCompile(`^[A-Za-z0-9_.+=:/@-]+$`)
)

func newInitCmd() *cobra.Command {
	var data struct {
		Cmds     []string
		SkipArgs []string
	}
	cmd := &cobra.Command{
		Use:       "init zsh",
		Short:     "Print shell code that sets up completion and aliases, for use with eval",
		Example:   `  eval "$(squelch init zsh --cmd gcloud)"`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"zsh"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, c := range data.Cmds {
				if !aliasName.MatchString(c) {
					return fmt.Errorf("invalid command name %q", c)
				}
			}
			for _, a := range data.SkipArgs {
				if !skipArg.MatchString(a) {
					return fmt.Errorf("invalid skip argument %q", a)
				}
			}
			out := cmd.OutOrStdout()
			if err := cmd.Root().GenZshCompletion(out); err != nil {
				return err
			}
			return initZshTmpl.Execute(out, data)
		},
	}
	cmd.Flags().StringArrayVar(&data.Cmds, "cmd", nil, "alias this command to run through squelch (repeatable)")
	cmd.Flags().StringArrayVar(&data.SkipArgs, "skip-arg", nil,
		"skip squelch when a wrapped command is given exactly this argument, such as --tui (repeatable)")
	return cmd
}
