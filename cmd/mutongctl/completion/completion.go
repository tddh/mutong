package completion

import (
	"os"

	"github.com/spf13/cobra"
)

func NewCmdCompletion(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `To load completions:

Bash:
  $ source <(mutongctl completion bash)
  # To load completions for each session:
  $ mutongctl completion bash > /usr/local/etc/bash_completion.d/mutongctl

Zsh:
  $ source <(mutongctl completion zsh)
  # If shell completion is not already enabled:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

Fish:
  $ mutongctl completion fish | source
  # To load completions for each session:
  $ mutongctl completion fish > ~/.config/fish/completions/mutongctl.fish

PowerShell:
  PS> mutongctl completion powershell | Out-String | Invoke-Expression`,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletion(os.Stdout)
			}
			return nil
		},
	}
	return cmd
}
