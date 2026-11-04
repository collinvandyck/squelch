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
)

func newInitCmd() *cobra.Command {
	var cmds []string
	cmd := &cobra.Command{
		Use:       "init zsh",
		Short:     "Print shell code that sets up completion and aliases, for use with eval",
		Example:   `  eval "$(squelch init zsh --cmd gcloud)"`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"zsh"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, c := range cmds {
				if !aliasName.MatchString(c) {
					return fmt.Errorf("invalid command name %q", c)
				}
			}
			out := cmd.OutOrStdout()
			if err := cmd.Root().GenZshCompletion(out); err != nil {
				return err
			}
			return initZshTmpl.Execute(out, cmds)
		},
	}
	cmd.Flags().StringArrayVar(&cmds, "cmd", nil, "alias this command to run through squelch (repeatable)")
	return cmd
}
