// Package config implements the `config` command tree for managing
// agent-deepweb's persistent user settings (~/.config/agent-deepweb/
// config.json).
package config

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shhac/agent-deepweb/internal/cli/shared"
	cfg "github.com/shhac/agent-deepweb/internal/config"
	agenterrors "github.com/shhac/agent-deepweb/internal/errors"
	"github.com/shhac/agent-deepweb/internal/output"
)

func Register(root *cobra.Command, _ shared.Globals) {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage agent-deepweb's persistent user config",
	}
	shared.LLMHelp(cmd, "config", usageText)

	cmd.AddCommand(&cobra.Command{
		Use:   "list-keys",
		Short: "List recognized config keys with current values + defaults",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := cfg.Read()
			type keyRow struct {
				Name        string `json:"name"`
				Kind        string `json:"kind"`
				Description string `json:"description"`
				Default     string `json:"default"`
				Value       string `json:"value"`
				Source      string `json:"source"`
			}
			rows := make([]keyRow, 0, len(cfg.Keys))
			for _, k := range cfg.Keys {
				val, src, _ := cfg.Get(c, k.Name)
				rows = append(rows, keyRow{
					Name:        k.Name,
					Kind:        k.Kind,
					Description: k.Description,
					Default:     k.Default,
					Value:       val,
					Source:      src,
				})
			}
			output.PrintJSON(map[string]any{"keys": rows})
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get <key>",
		Short: "Print the current value of a config key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := cfg.Read()
			val, src, err := cfg.Get(c, args[0])
			if err != nil {
				return shared.Fail(unknownKeyError(args[0]))
			}
			output.PrintJSON(map[string]any{"key": args[0], "value": val, "source": src})
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "set <key> <value>",
		Short: "Persist a config key value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := cfg.Read()
			if err := cfg.Set(c, args[0], args[1]); err != nil {
				if errors.Is(err, cfg.ErrUnknownKey) {
					return shared.Fail(unknownKeyError(args[0]))
				}
				return shared.Fail(agenterrors.Newf(agenterrors.FixableByAgent, "%s", err.Error()))
			}
			if err := cfg.Write(c); err != nil {
				return shared.FailHuman(err)
			}
			// Re-read so applyDefaults runs and Get reports the effective
			// value (not the pre-Write in-memory copy).
			val, src, _ := cfg.Get(cfg.Read(), args[0])
			shared.PrintOK(map[string]any{"key": args[0], "value": val, "source": src})
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "unset <key>",
		Short: "Revert a config key to the built-in default",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := cfg.Read()
			if err := cfg.Unset(c, args[0]); err != nil {
				return shared.Fail(unknownKeyError(args[0]))
			}
			if err := cfg.Write(c); err != nil {
				return shared.FailHuman(err)
			}
			val, src, _ := cfg.Get(cfg.Read(), args[0])
			shared.PrintOK(map[string]any{"key": args[0], "value": val, "source": src})
			return nil
		},
	})

	root.AddCommand(cmd)
}

// unknownKeyError produces a fixable_by:agent error naming the valid
// key set, so the user / LLM can correct a typo without running
// another command.
func unknownKeyError(got string) error {
	names := make([]string, 0, len(cfg.Keys))
	for _, k := range cfg.Keys {
		names = append(names, k.Name)
	}
	return agenterrors.Newf(agenterrors.FixableByAgent,
		"unknown config key %q", got).
		WithHint(fmt.Sprintf("valid keys: %v (see 'agent-deepweb config list-keys')", names))
}

const usageText = `config — persistent user config

USAGE
  agent-deepweb config list-keys
  agent-deepweb config get <key>
  agent-deepweb config set <key> <value>
  agent-deepweb config unset <key>

SUMMARY
  Config is persisted at ~/.config/agent-deepweb/config.json. Precedence
  at call time is: per-invocation flag > config value > built-in default.
  Only AGENT_DEEPWEB_CONFIG_DIR remains as an environment variable (it
  points at the config dir itself, used by tests).

KEYS
  default.timeout-ms   Default request timeout (ms)            [built-in: 30000]
  default.max-bytes    Default response body size cap (bytes)  [built-in: 10485760]
  default.user-agent   Fallback User-Agent                     [built-in: agent-deepweb/<version>]
  default.profile      Fallback profile when --profile omitted [built-in: unset]
  audit.enabled        Write the audit log                      [built-in: true]
  track.ttl            How long tracked records live            [built-in: 168h]

PER-INVOCATION FLAGS
  Most keys have a matching flag that overrides config for one call:
    --timeout <ms>           → default.timeout-ms
    --max-size <bytes>       → default.max-bytes   (on fetch)
    --user-agent <s>         → default.user-agent  (on fetch)
    --profile <name>         → default.profile
    --track-ttl <duration>   → track.ttl           (on fetch/graphql/template run)

  audit.enabled is global; toggle via 'config set audit.enabled false'.
`
