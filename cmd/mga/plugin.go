package main

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/shinucla/mini-golang-agent/internal/agent"
	"github.com/shinucla/mini-golang-agent/internal/agentdef"
	"github.com/shinucla/mini-golang-agent/internal/mcp"
	"github.com/shinucla/mini-golang-agent/internal/plugin"
	"github.com/shinucla/mini-golang-agent/internal/tools"
)

const pluginUsage = `usage:
  mga plugin marketplace add <git url | owner/repo | folder>
  mga plugin marketplace list
  mga plugin marketplace update [name]
  mga plugin marketplace remove <name>
  mga plugin list [--available]
  mga plugin install <plugin>[@marketplace]
  mga plugin uninstall <plugin>[@marketplace]
  mga plugin enable <plugin>[@marketplace]
  mga plugin disable <plugin>[@marketplace]
  mga plugin update [plugin[@marketplace]]`

func pluginCommand(args []string) error {
	return pluginTo(os.Stdout, args)
}

func pluginTo(out io.Writer, args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	sub, rest := args[0], args[1:]
	one := func() (string, error) {
		if len(rest) != 1 {
			return "", errors.New(pluginUsage)
		}
		return rest[0], nil
	}
	switch sub {
	case "marketplace", "marketplaces":
		return marketplaceTo(out, rest)
	case "list", "ls":
		if slices.Contains(rest, "--available") {
			return listAvailable(out)
		}
		return listInstalled(out)
	case "install", "i":
		id, err := one()
		if err != nil {
			return err
		}
		rec, err := plugin.Install(id)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Installed %s %s\n", rec.ID, rec.Version)
		return describe(out, rec.ID)
	case "uninstall", "remove", "rm":
		id, err := one()
		if err != nil {
			return err
		}
		if err := plugin.Uninstall(id); err != nil {
			return err
		}
		fmt.Fprintf(out, "Uninstalled %s\n", id)
		return nil
	case "enable", "disable":
		id, err := one()
		if err != nil {
			return err
		}
		full, err := plugin.SetEnabled(id, sub == "enable")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%sd %s; restart mga to apply it\n", strings.ToUpper(sub[:1])+sub[1:], full)
		return nil
	case "update":
		ids := rest
		if len(ids) == 0 {
			installed, err := plugin.InstalledPlugins()
			if err != nil {
				return err
			}
			ids = slices.Sorted(maps.Keys(installed))
		}
		var errs []error
		for _, id := range ids {
			rec, err := plugin.Update(id)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", id, err))
				continue
			}
			fmt.Fprintf(out, "Updated %s to %s\n", rec.ID, rec.Version)
		}
		return errors.Join(errs...)
	}
	return errors.New(pluginUsage)
}

func marketplaceTo(out io.Writer, args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "add":
		if len(args) != 2 {
			return errors.New("usage: mga plugin marketplace add <git url | owner/repo | folder>")
		}
		mk, err := plugin.AddMarketplace(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Added marketplace %s (%s)\n", mk.Name, mk.Source)
		_, m, err := plugin.ReadMarketplace(mk.Name)
		if err != nil {
			return err
		}
		for _, e := range m.Plugins {
			fmt.Fprintf(out, "  %s@%s  %s\n", e.Name, mk.Name, tools.OneLine(e.Description, 90))
		}
		fmt.Fprintf(out, "Install one with: mga plugin install <plugin>@%s\n", mk.Name)
		return nil
	case "list", "ls":
		all, err := plugin.Marketplaces()
		if err != nil {
			return err
		}
		if len(all) == 0 {
			fmt.Fprintln(out, "No marketplaces. Add one with: mga plugin marketplace add <git url>")
			return nil
		}
		for _, name := range slices.Sorted(maps.Keys(all)) {
			mk := all[name]
			fmt.Fprintf(out, "%s  %s  (updated %s)\n", mk.Name, mk.Source, mk.Updated.Format("2006-01-02 15:04"))
		}
		return nil
	case "update":
		names := args[1:]
		if len(names) == 0 {
			all, err := plugin.Marketplaces()
			if err != nil {
				return err
			}
			names = slices.Sorted(maps.Keys(all))
		}
		var errs []error
		for _, name := range names {
			if err := plugin.UpdateMarketplace(name); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", name, err))
				continue
			}
			fmt.Fprintf(out, "Updated marketplace %s\n", name)
		}
		return errors.Join(errs...)
	case "remove", "rm":
		if len(args) != 2 {
			return errors.New("usage: mga plugin marketplace remove <name>")
		}
		removed, err := plugin.RemoveMarketplace(args[1])
		for _, id := range removed {
			fmt.Fprintf(out, "Uninstalled %s\n", id)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Removed marketplace %s\n", args[1])
		return nil
	}
	return errors.New(pluginUsage)
}

func listInstalled(out io.Writer) error {
	loaded, err := plugin.LoadAll()
	if err != nil {
		return err
	}
	if len(loaded.Plugins) == 0 {
		fmt.Fprintln(out, "No plugins installed. See: mga plugin list --available")
		return nil
	}
	for _, p := range loaded.Plugins {
		state := "enabled"
		if !p.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(out, "%s  %s  %s\n  %s\n", p.ID, p.Version, state, pluginSummary(p))
		if p.Err != nil {
			fmt.Fprintf(out, "  problem: %s\n", tools.OneLine(p.Err.Error(), 300))
		}
	}
	return nil
}

func describe(out io.Writer, id string) error {
	loaded, err := plugin.LoadAll()
	if err != nil {
		return err
	}
	for _, p := range loaded.Plugins {
		if p.ID != id {
			continue
		}
		fmt.Fprintf(out, "  %s\n", pluginSummary(p))
		for _, s := range p.Skills {
			fmt.Fprintf(out, "  skill   /%s\n", s)
		}
		for _, c := range p.Commands {
			fmt.Fprintf(out, "  command /%s\n", c)
		}
		for _, a := range p.Agents {
			fmt.Fprintf(out, "  agent   %s\n", a)
		}
		for _, s := range p.MCP {
			fmt.Fprintf(out, "  MCP     %s\n", s)
		}
		if p.Err != nil {
			fmt.Fprintf(out, "  problem: %s\n", tools.OneLine(p.Err.Error(), 300))
		}
	}
	return nil
}

func pluginSummary(p plugin.Info) string {
	parts := []string{fmt.Sprintf("%d skills", len(p.Skills)), fmt.Sprintf("%d commands", len(p.Commands)), fmt.Sprintf("%d agents", len(p.Agents)), fmt.Sprintf("%d MCP servers", len(p.MCP))}
	if p.Hooks {
		parts = append(parts, "hooks (not supported in mga)")
	}
	return strings.Join(parts, " · ")
}

func listAvailable(out io.Writer) error {
	avail, err := plugin.AvailablePlugins()
	if len(avail) == 0 && err == nil {
		fmt.Fprintln(out, "No marketplaces. Add one with: mga plugin marketplace add <git url>")
	}
	for _, a := range avail {
		state := ""
		switch {
		case a.Enabled:
			state = "  [installed]"
		case a.Installed:
			state = "  [installed, disabled]"
		}
		fmt.Fprintf(out, "%s%s\n  %s\n", a.ID, state, tools.OneLine(a.Description, 120))
	}
	return err
}

func applyPlugins(rt *agent.Runtime, servers *mcp.Manager) []agentdef.Definition {
	loaded, err := plugin.LoadEnabled()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mga: plugins:", err)
		return nil
	}
	for _, s := range loaded.Skills {
		rt.Skills = append(rt.Skills, tools.SkillInfo{Name: s.Name, Description: s.Description, Dir: s.Dir, Load: func() (string, error) { return plugin.Body(s.File) }})
	}
	for _, c := range loaded.Commands {
		rt.Commands = append(rt.Commands, agent.UserCommand{Name: c.Name, Description: c.Description, ArgHint: c.ArgHint, Load: func() (string, error) { return plugin.Body(c.File) }})
	}
	rt.Defs.Plugins = loaded.Agents
	if servers != nil {
		servers.AddServers(loaded.MCP, mcp.ScopePlugin)
	}
	return loaded.Agents
}
